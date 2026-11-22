package paymentservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"cityflow/app/payment/domain/payment"
	"cityflow/internal/common/domain"
)

func (s *PaymentService) Summary(ctx context.Context, taskID string) (*PaymentSummary, error) {
	summary := &PaymentSummary{}
	order, err := s.orders.PaymentOrderByTask(ctx, taskID)
	switch {
	case err == nil:
		summary.Payment = order
	case !errors.Is(err, domain.ErrNotFound):
		return nil, err
	}
	settlement, err := s.orders.SettlementByTask(ctx, taskID)
	switch {
	case err == nil:
		summary.Settlement = settlement
	case !errors.Is(err, domain.ErrNotFound):
		return nil, err
	}
	return summary, nil
}

// HandleNotify applies a signature-checked provider callback. It is safe to
// call repeatedly: paid, closed and refunded states are all compare-and-set.
func (s *PaymentService) HandleNotify(ctx context.Context, provider string, headers map[string]string, body []byte) error {
	provider = strings.ToUpper(strings.TrimSpace(provider))
	gateway, ok := s.gateways[provider]
	if !ok {
		return fmt.Errorf("%w: unknown provider %q", domain.ErrInvalidRequest, provider)
	}
	now := s.now()
	notification, err := gateway.ParseNotify(ctx, headers, body)
	if err != nil {
		s.recordNotify(ctx, payment.NotifyLog{
			Provider: provider, Result: "rejected", Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
		})
		return err
	}
	if notification.Kind == payment.NotifyKindPayout {
		return s.applyPayoutNotify(ctx, provider, notification, body, now)
	}
	if notification.Kind == payment.NotifyKindRefund {
		return s.applyRefundNotify(ctx, provider, notification, body, now)
	}
	entry := payment.NotifyLog{
		Provider: provider, ProviderOrderNo: notification.OrderNo, ProviderTradeID: notification.TradeID,
		SignatureValid: true, Status: notification.Status, Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
	}
	order, err := s.orders.PaymentOrderByProviderNo(ctx, provider, notification.OrderNo)
	if err != nil {
		entry.Result = "unknown_order"
		s.recordNotify(ctx, entry)
		return err
	}
	if notification.AmountFen > 0 && notification.AmountFen != order.AmountFen {
		entry.Result = "amount_mismatch"
		s.recordNotify(ctx, entry)
		return fmt.Errorf("%w: provider amount %d does not match order amount %d", domain.ErrConflict, notification.AmountFen, order.AmountFen)
	}
	switch notification.Status {
	case payment.PaymentPaid:
		paidAt := derefTime(notification.PaidAt, now)
		paidOrder, payErr := s.orders.MarkPaymentPaid(ctx, order.ID, notification.TradeID, paidAt)
		if payErr != nil {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return payErr
		}
		s.noteCouponUsed(ctx, paidOrder)
		entry.Result = "paid"
	case payment.PaymentClosed:
		if _, err := s.orders.ClosePaymentOrder(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		s.releaseCoupon(ctx, order)
		entry.Result = "closed"
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refunded"
	case payment.PaymentRefundFailed:
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, "provider reported a refund failure"); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refund_failed"
	default:
		entry.Result = "ignored"
	}
	if err := s.orders.NotePaymentNotify(ctx, order.ID, now); err != nil {
		log.Printf("warning: note notify for payment %s: %v", order.ID, err)
	}
	s.recordNotify(ctx, entry)
	return nil
}

func (s *PaymentService) applyPayoutNotify(ctx context.Context, provider string, notification *payment.Notification, body []byte, now time.Time) error {
	entry := payment.NotifyLog{
		Provider: provider, ProviderTradeID: notification.TradeID, SignatureValid: true,
		Status: notification.Status, Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
	}
	settlement, err := s.orders.SettlementByProviderBillNo(ctx, provider, notification.BillNo)
	if err != nil {
		entry.Result = "unknown_bill"
		s.recordNotify(ctx, entry)
		return err
	}
	switch notification.Status {
	case payment.SettlementPaid:
		if _, err := s.orders.MarkSettlementPaid(ctx, settlement.ID, notification.TradeID, derefTime(notification.PaidAt, now)); err != nil {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "settled"
	case payment.SettlementFailed:
		if _, err := s.orders.MarkSettlementFailed(ctx, settlement.ID, "provider reported a failed transfer", nil); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "failed"
	default:
		entry.Result = "processing"
	}
	s.recordNotify(ctx, entry)
	return nil
}

func (s *PaymentService) applyRefundNotify(ctx context.Context, provider string, notification *payment.Notification, body []byte, now time.Time) error {
	entry := payment.NotifyLog{
		Provider: provider, ProviderOrderNo: notification.OrderNo, ProviderTradeID: notification.TradeID,
		SignatureValid: true, Status: notification.Status, Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
	}
	order, err := s.orders.PaymentOrderByProviderNo(ctx, provider, notification.OrderNo)
	if err != nil {
		entry.Result = "unknown_order"
		s.recordNotify(ctx, entry)
		return err
	}
	if notification.AmountFen > 0 && notification.AmountFen != order.AmountFen {
		entry.Result = "amount_mismatch"
		s.recordNotify(ctx, entry)
		return fmt.Errorf("%w: refund amount %d does not match order amount %d", domain.ErrConflict, notification.AmountFen, order.AmountFen)
	}
	switch notification.Status {
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refunded"
	case payment.PaymentRefundFailed:
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, "provider reported a refund failure"); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refund_failed"
	default:
		entry.Result = "processing"
	}
	if err := s.orders.NotePaymentNotify(ctx, order.ID, now); err != nil {
		log.Printf("warning: note refund notify for payment %s: %v", order.ID, err)
	}
	s.recordNotify(ctx, entry)
	return nil
}

func (s *PaymentService) recordNotify(ctx context.Context, entry payment.NotifyLog) {
	if s.notifyBuffer != nil && s.notifyBuffer.Enqueue(entry) {
		return
	}
	if err := s.orders.RecordNotify(ctx, entry); err != nil {
		log.Printf("warning: record payment notify: %v", err)
	}
}
