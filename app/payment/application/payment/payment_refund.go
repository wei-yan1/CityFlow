package paymentservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cityflow/app/payment/domain/payment"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
)

// SimulatePaid is the mock-only shortcut. It is refused unless the order was
// created by the mock gateway and the deployment explicitly allows it.
func (s *PaymentService) SimulatePaid(ctx context.Context, orderID, payerID string) (*payment.PaymentOrder, error) {
	if !s.allowMockPay {
		return nil, domain.ErrForbidden
	}
	order, err := s.orders.PaymentOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.Provider != payment.ProviderMock {
		return nil, fmt.Errorf("%w: only mock payments can be simulated", domain.ErrForbidden)
	}
	if order.PayerID != payerID {
		return nil, domain.ErrForbidden
	}
	paid, err := s.orders.MarkPaymentPaid(ctx, order.ID, "MOCK-"+order.ID, s.now())
	if err != nil {
		return nil, err
	}
	s.noteCouponUsed(ctx, paid)
	return paid, nil
}

// SyncPayment actively queries the provider; webhooks can be delayed or lost,
// so the client and the reconciliation job both need this path.
func (s *PaymentService) SyncPayment(ctx context.Context, orderID, actorID, role string) (*payment.PaymentOrder, error) {
	order, err := s.orders.PaymentOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && order.PayerID != actorID {
		return nil, domain.ErrForbidden
	}
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		return nil, err
	}
	state, err := gateway.Query(ctx, order.ProviderOrderNo)
	if err != nil {
		return nil, fmt.Errorf("%w: 查询支付状态失败: %v", domain.ErrUnavailable, err)
	}
	switch state.Status {
	case payment.PaymentPaid:
		if state.AmountFen > 0 && state.AmountFen != order.AmountFen {
			return nil, fmt.Errorf("%w: 渠道返回金额 %d 分与支付单金额 %d 分不一致", domain.ErrConflict, state.AmountFen, order.AmountFen)
		}
		paidOrder, payErr := s.orders.MarkPaymentPaid(ctx, order.ID, state.TradeID, derefTime(state.PaidAt, s.now()))
		if payErr != nil {
			if !errors.Is(payErr, domain.ErrConflict) {
				return nil, payErr
			}
		} else {
			s.noteCouponUsed(ctx, paidOrder)
		}
	case payment.PaymentClosed:
		if _, err := s.orders.ClosePaymentOrder(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
		s.releaseCoupon(ctx, order)
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
	}
	return s.orders.PaymentOrder(ctx, order.ID)
}

func (s *PaymentService) Refund(ctx context.Context, orderID, actorID, role, reason string) (*payment.PaymentOrder, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "退款"
	}
	reason = truncateRunes(reason, 255)
	order, err := s.orders.PaymentOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && order.PayerID != actorID {
		return nil, domain.ErrForbidden
	}
	if order.Status == payment.PaymentRefunded {
		return order, nil
	}
	switch order.Status {
	case payment.PaymentPaid, payment.PaymentRefundPending, payment.PaymentRefundFailed:
	default:
		return nil, fmt.Errorf("%w: 只有已支付的支付单可以退款", domain.ErrConflict)
	}
	if settlement, err := s.orders.SettlementByTask(ctx, order.TaskID); err == nil {
		switch settlement.Status {
		case payment.SettlementProcessing, payment.SettlementPaid, payment.SettlementUnknown:
			return nil, fmt.Errorf("%w: 骑手结算已开始或状态待核对，请先联系平台处理", domain.ErrConflict)
		case payment.SettlementPending:
			// Payout has not been claimed yet. Cancel it with a compare-and-set
			// first; if the worker has already claimed it, the refund is refused
			// rather than paying the rider and refunding the user twice.
			if _, err := s.orders.CancelPendingSettlement(ctx, settlement.ID, "退款前取消结算"); err != nil {
				if errors.Is(err, domain.ErrConflict) {
					return nil, fmt.Errorf("%w: 骑手结算已开始，请先联系平台处理", domain.ErrConflict)
				}
				return nil, err
			}
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		return nil, err
	}
	if _, err := s.orders.MarkPaymentRefundPending(ctx, order.ID, reason); err != nil && !errors.Is(err, domain.ErrConflict) {
		return nil, err
	}
	refund, err := gateway.Refund(ctx, payment.RefundRequest{
		OrderNo: order.ProviderOrderNo, RefundNo: refundNumber(order),
		TradeID: order.ProviderTradeID, AmountFen: order.AmountFen, Reason: reason,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: 退款失败: %v", domain.ErrUnavailable, err)
	}
	switch refund.Status {
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil {
			return nil, err
		}
	case payment.PaymentRefundFailed:
		reason := firstNonEmptyText(refund.FailReason, "provider rejected the refund")
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, truncateRunes(reason, 250)); err != nil && !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
	}
	return s.orders.PaymentOrder(ctx, order.ID)
}

func refundNumber(order *payment.PaymentOrder) string {
	prefix := "RF-" + order.ProviderOrderNo
	if len(prefix) > 64 {
		prefix = "RF-" + order.ID
	}
	return prefix
}
