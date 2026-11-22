package paymentservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"cityflow/app/payment/domain/payment"
	"cityflow/internal/common/domain"
)

// EnsureSettlement creates the payout record once a task is completed and its
// payment is verified. Its idempotency key is the task id, so a retry can never
// create a second payout for the same job.
func (s *PaymentService) EnsureSettlement(ctx context.Context, item *TaskView) (*payment.Settlement, error) {
	if item == nil || item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 || item.WorkerID == "" {
		return nil, nil
	}
	if existing, err := s.orders.SettlementByTask(ctx, item.ID); err == nil {
		return existing, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if order.Status != payment.PaymentPaid {
		return nil, nil
	}
	payoutFen, feeFen := payment.SplitAmount(*item.QuotedAmountFen, s.feeBPS)
	if payoutFen <= 0 {
		return nil, nil
	}
	id, err := payment.NewID()
	if err != nil {
		return nil, err
	}
	billNo, err := payment.NewProviderOrderNo("ST")
	if err != nil {
		return nil, err
	}
	settlement, err := s.orders.CreateSettlement(ctx, payment.Settlement{
		ID: id, TaskID: item.ID, PaymentOrderID: order.ID, RiderID: item.WorkerID,
		AmountFen: payoutFen, FeeFen: feeFen, Currency: payment.CurrencyCNY, Status: payment.SettlementPending,
		Provider: order.Provider, ProviderBillNo: billNo, IdempotencyKey: "task:" + item.ID,
	})
	if err != nil {
		return nil, err
	}
	return settlement, nil
}

// SettleOnce claims a small batch of due payouts and attempts each transfer.
// It is called by the worker and by tests; it is safe to run concurrently
// because claiming uses SKIP LOCKED in MySQL.
func (s *PaymentService) SettleOnce(ctx context.Context) (int, error) {
	due, err := s.orders.ClaimDueSettlements(ctx, s.now(), 10, 2*time.Minute)
	if err != nil {
		return 0, err
	}
	for _, item := range due {
		if err := s.settleOne(ctx, item); err != nil {
			log.Printf("warning: settle %s: %v", item.ID, err)
		}
	}
	return len(due), nil
}

func (s *PaymentService) settleOne(ctx context.Context, item *payment.Settlement) error {
	gateway, ok := s.gateways[item.Provider]
	if !ok || !gateway.Configured() {
		return s.retryLater(ctx, item, "支付渠道未配置")
	}
	// The mock channel exists so the whole flow can be demonstrated without
	// merchant credentials; it has no real payee, so it needs no payout account.
	account := payment.PayoutAccount{Provider: payment.ProviderMock, Account: "mock", AccountName: "mock rider"}
	if item.Provider != payment.ProviderMock {
		stored, err := s.orders.PayoutAccount(ctx, item.RiderID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return s.retryLater(ctx, item, "骑手未设置收款账户")
			}
			return err
		}
		if stored.Provider != item.Provider {
			return s.retryLater(ctx, item, fmt.Sprintf("骑手收款账户属于 %s，与支付渠道 %s 不一致", stored.Provider, item.Provider))
		}
		account = *stored
	}
	// On a retry, ask the provider first. A transfer that is merely slow must
	// not be sent twice, and a success that arrived after our timeout must be
	// booked instead of retried until it is marked failed.
	if item.Attempts > 1 {
		if query, ok := gateway.(payment.PayoutQuerier); ok {
			if payout, err := query.QueryPayout(ctx, item.ProviderBillNo); err == nil {
				switch payout.Status {
				case payment.SettlementPaid:
					_, err := s.orders.MarkSettlementPaid(ctx, item.ID, firstNonEmptyText(payout.BillNo, item.ProviderBillNo), s.now())
					return err
				case payment.SettlementFailed:
					return s.retryLater(ctx, item, firstNonEmptyText(payout.FailReason, "provider reported a failed transfer"))
				case payment.SettlementProcessing:
					log.Printf("settlement %s is still processing at the provider; leaving it for the next lease", item.ID)
					return nil
				}
			} else {
				log.Printf("warning: query payout %s before retry: %v; retrying with the same bill number", item.ID, err)
			}
		}
	}
	payout, err := gateway.Transfer(ctx, payment.PayoutRequest{
		BillNo: item.ProviderBillNo, OrderNo: "", RiderID: item.RiderID,
		AmountFen: item.AmountFen, Account: account, Remark: "CityFlow 任务结算 " + item.TaskID,
	})
	if err != nil {
		return s.retryLaterAfterTransfer(ctx, item, err.Error())
	}
	switch payout.Status {
	case payment.SettlementPaid:
		_, err := s.orders.MarkSettlementPaid(ctx, item.ID, payout.BillNo, s.now())
		return err
	case payment.SettlementFailed:
		return s.retryLater(ctx, item, firstNonEmptyText(payout.FailReason, "provider reported a failed transfer"))
	default:
		return s.retryLaterAfterTransfer(ctx, item, "provider is still processing the transfer")
	}
}

func (s *PaymentService) retryLater(ctx context.Context, item *payment.Settlement, reason string) error {
	return s.retryLaterAfter(ctx, item, reason, false)
}

// retryLaterAfterTransfer is used when the provider may already have accepted
// the transfer (timeout or "processing"). At the last attempt it must query the
// provider instead of marking FAILED, otherwise a later refund could pay the
// user while the rider has already received the money.
func (s *PaymentService) retryLaterAfterTransfer(ctx context.Context, item *payment.Settlement, reason string) error {
	return s.retryLaterAfter(ctx, item, reason, true)
}

func (s *PaymentService) retryLaterAfter(ctx context.Context, item *payment.Settlement, reason string, mayHaveLeft bool) error {
	if item.Attempts >= s.maxAttempts {
		if !mayHaveLeft {
			_, err := s.orders.MarkSettlementFailed(ctx, item.ID, truncateRunes(reason, 250), nil)
			return err
		}
		gateway, ok := s.gateways[item.Provider]
		if !ok {
			return s.markSettlementUnknown(ctx, item, "渠道不可用，无法确认打款最终状态")
		}
		query, ok := gateway.(payment.PayoutQuerier)
		if !ok {
			return s.markSettlementUnknown(ctx, item, "渠道不支持打款回查，无法确认最终状态")
		}
		payout, err := query.QueryPayout(ctx, item.ProviderBillNo)
		if err != nil {
			return s.markSettlementUnknown(ctx, item, "无法确认打款最终状态: "+err.Error())
		}
		switch payout.Status {
		case payment.SettlementPaid:
			_, err := s.orders.MarkSettlementPaid(ctx, item.ID, firstNonEmptyText(payout.BillNo, item.ProviderBillNo), s.now())
			return err
		case payment.SettlementFailed:
			_, err := s.orders.MarkSettlementFailed(ctx, item.ID, truncateRunes(firstNonEmptyText(payout.FailReason, reason), 250), nil)
			return err
		default:
			return s.markSettlementUnknown(ctx, item, "渠道仍在处理打款，需人工核对")
		}
	}
	backoff := time.Duration(1<<uint(minInt(item.Attempts-1, 8))) * time.Minute
	if backoff > 6*time.Hour {
		backoff = 6 * time.Hour
	}
	retryAt := s.now().Add(backoff)
	_, err := s.orders.MarkSettlementFailed(ctx, item.ID, truncateRunes(reason, 250), &retryAt)
	return err
}

func (s *PaymentService) markSettlementUnknown(ctx context.Context, item *payment.Settlement, reason string) error {
	log.Printf("warning: settlement %s state is unknown, manual reconciliation required: %s", item.ID, reason)
	_, err := s.orders.MarkSettlementUnknown(ctx, item.ID, truncateRunes(reason, 250))
	return err
}
