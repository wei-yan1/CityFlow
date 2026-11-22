package paymentservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"cityflow/app/payment/domain/payment"
	"cityflow/app/payment/repository"
	"cityflow/internal/common/domain"
)

func (s *PaymentService) SettlementsForRider(ctx context.Context, riderID string, page repository.Page) ([]*payment.Settlement, error) {
	return s.orders.SettlementsByRider(ctx, riderID, page)
}

func (s *PaymentService) PayoutAccount(ctx context.Context, riderID string) (*payment.PayoutAccount, error) {
	return s.orders.PayoutAccount(ctx, riderID)
}

func (s *PaymentService) SavePayoutAccount(ctx context.Context, riderID string, request payment.PayoutAccountRequest) (*payment.PayoutAccount, error) {
	provider := strings.ToUpper(strings.TrimSpace(request.Provider))
	switch provider {
	case payment.ProviderAlipay, payment.ProviderWechat, "BANK":
	default:
		return nil, fmt.Errorf("%w: 收款渠道必须是 ALIPAY、WECHAT 或 BANK", domain.ErrInvalidRequest)
	}
	account := strings.TrimSpace(request.Account)
	if account == "" || len([]rune(account)) > 128 {
		return nil, fmt.Errorf("%w: 收款账户不能为空且不超过 128 个字符", domain.ErrInvalidRequest)
	}
	name := strings.TrimSpace(request.AccountName)
	if len([]rune(name)) > 80 {
		return nil, fmt.Errorf("%w: 收款人姓名过长", domain.ErrInvalidRequest)
	}
	return s.orders.SavePayoutAccount(ctx, payment.PayoutAccount{
		RiderID: riderID, Provider: provider, Account: account, AccountName: name,
	})
}

// EnsureClaimable is the gate plugged into TaskService.Claim: a quoted task
// can only be taken after its payment is verified.
func (s *PaymentService) EnsureClaimable(ctx context.Context, item *TaskView) error {
	if item == nil || item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
		return nil
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: 请先完成支付后再接单", domain.ErrConflict)
	}
	if err != nil {
		return err
	}
	if order.Status != payment.PaymentPaid {
		return fmt.Errorf("%w: 任务尚未收到付款，暂不能接单", domain.ErrConflict)
	}
	return nil
}

// EnsureEditable blocks changing a task's amount after a payment attempt
// exists, which would otherwise desynchronise the paid amount and the task.
func (s *PaymentService) EnsureEditable(ctx context.Context, item *TaskView, nextAmount *int64) error {
	if item == nil || item.QuotedAmountFen == nil {
		return nil
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if order.Status != payment.PaymentPending && order.Status != payment.PaymentPaid {
		return nil
	}
	if nextAmount == nil || *nextAmount != order.AmountFen {
		return fmt.Errorf("%w: 已发起支付，任务金额不能修改", domain.ErrConflict)
	}
	return nil
}

// AfterCancel refunds a paid task in the background. The task is already
// cancelled at this point, so a provider failure must not resurrect it.
func (s *PaymentService) AfterCancel(ctx context.Context, item *TaskView) {
	if err := s.RefundCancelledTask(ctx, item); err != nil {
		log.Printf("warning: refund cancelled task: %v", err)
	}
}
