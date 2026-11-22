package paymentservice

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"cityflow/app/payment/domain/payment"
	"cityflow/app/payment/repository"
	"cityflow/internal/common/domain"
)

// RunWorker runs the scalable payout loop alongside the singleton-style
// reconciliation scans. Deployments may run the payout loop on every Payment
// instance because claims use SKIP LOCKED and a lease, while electing one
// instance to run the scans.
func (s *PaymentService) RunWorker(ctx context.Context, interval time.Duration) {
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		s.RunSettlementWorker(ctx, interval)
	}()
	go func() {
		defer workers.Done()
		s.RunReconciliationWorker(ctx, interval)
	}()
	go func() {
		defer workers.Done()
		s.RunInboxRecoveryWorker(ctx, interval)
	}()
	workers.Wait()
}

// RunSettlementWorker can run on multiple instances. ClaimDueSettlements
// atomically leases disjoint batches using FOR UPDATE SKIP LOCKED.
func (s *PaymentService) RunSettlementWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if _, err := s.SettleOnce(ctx); err != nil {
		log.Printf("warning: settlement worker: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SettleOnce(ctx); err != nil {
				log.Printf("warning: settlement worker: %v", err)
			}
		}
	}
}

// RunReconciliationWorker scans payment/refund states and repairs missed
// settlement creation. Keep this on one elected instance until each scan has
// its own row claims/leases; these scans can call external channels.
func (s *PaymentService) RunReconciliationWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.reconcile(ctx)
	s.ReconcileOnce(ctx)
	s.purgeInboxIfDue(ctx)
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ticks++
			if ticks%4 == 0 {
				s.ReconcileOnce(ctx)
				s.reconcile(ctx)
				s.purgeInboxIfDue(ctx)
			}
		}
	}
}

// purgeInboxIfDue deletes old PROCESSED inbox rows. Keeping every historical
// event makes deduplication slower and the table larger; 30 days is longer than
// any broker replay/restore window we rely on.
func (s *PaymentService) purgeInboxIfDue(ctx context.Context) {
	if s == nil || s.inbox == nil {
		return
	}
	now := s.now()
	if !s.lastInboxPurge.IsZero() && now.Sub(s.lastInboxPurge) < inboxPurgeEvery {
		return
	}
	s.lastInboxPurge = now
	total, err := s.PurgeInboxJob(ctx)
	if err != nil {
		log.Printf("warning: purge payment inbox: %v", err)
		return
	}
	if total > 0 {
		log.Printf("payment inbox purge: deleted %d processed rows older than %s", total, inboxRetention)
	}
}

// ReconcileOnce is the scheduled safety net that a webhook cannot replace:
// expiring payments are queried and closed, and refunds left in
// REFUND_PENDING are queried or retried with the same refund number.
func (s *PaymentService) ReconcileOnce(ctx context.Context) (int, int) {
	now := s.now()
	payments, err := s.orders.PendingPayments(ctx, now.Add(-time.Minute), 20)
	if err != nil {
		log.Printf("warning: reconcile pending payments: %v", err)
		payments = nil
	}
	checkedPayments := 0
	for _, order := range payments {
		checkedPayments++
		s.reconcilePayment(ctx, order, now)
	}

	refunds, err := s.orders.RefundPendingPayments(ctx, now.Add(-time.Minute), 20)
	if err != nil {
		log.Printf("warning: reconcile pending refunds: %v", err)
		refunds = nil
	}
	checkedRefunds := 0
	for _, order := range refunds {
		checkedRefunds++
		s.reconcileRefund(ctx, order)
	}

	// REFUND_FAILED is terminal for the synchronous caller but not for the
	// scheduled safety net. Retry a bounded batch after a cooldown; the provider
	// refund number is stable, so resubmission is idempotent.
	failedRefunds, err := s.orders.RefundFailedPayments(ctx, now.Add(-5*time.Minute), 20)
	if err != nil {
		log.Printf("warning: reconcile failed refunds: %v", err)
		failedRefunds = nil
	}
	for _, order := range failedRefunds {
		checkedRefunds++
		s.reconcileRefund(ctx, order)
	}
	return checkedPayments, checkedRefunds
}

func (s *PaymentService) reconcilePayment(ctx context.Context, order *payment.PaymentOrder, now time.Time) {
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		log.Printf("warning: reconcile payment %s: %v", order.ID, err)
		return
	}
	state, err := gateway.Query(ctx, order.ProviderOrderNo)
	if err != nil {
		log.Printf("warning: query payment %s: %v", order.ID, err)
		return
	}
	switch state.Status {
	case payment.PaymentPaid:
		if state.AmountFen > 0 && state.AmountFen != order.AmountFen {
			log.Printf("warning: payment %s amount mismatch: provider=%d local=%d", order.ID, state.AmountFen, order.AmountFen)
			return
		}
		if paidOrder, payErr := s.orders.MarkPaymentPaid(ctx, order.ID, state.TradeID, derefTime(state.PaidAt, now)); payErr != nil {
			if !errors.Is(payErr, domain.ErrConflict) {
				log.Printf("warning: reconcile payment %s paid: %v", order.ID, payErr)
			}
		} else {
			s.noteCouponUsed(ctx, paidOrder)
		}
	case payment.PaymentClosed:
		if _, err := s.orders.ClosePaymentOrder(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: reconcile payment %s closed: %v", order.ID, err)
		}
		s.releaseCoupon(ctx, order)
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: reconcile payment %s refunded: %v", order.ID, err)
		}
	default:
		if order.ExpiresAt != nil && !order.ExpiresAt.After(now) {
			if err := s.closeProviderOrder(ctx, gateway, order); err != nil {
				log.Printf("warning: expire payment %s: %v", order.ID, err)
			}
		}
	}
}

func (s *PaymentService) reconcileRefund(ctx context.Context, order *payment.PaymentOrder) {
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		log.Printf("warning: reconcile refund %s: %v", order.ID, err)
		return
	}
	number := refundNumber(order)
	if query, ok := gateway.(payment.RefundQuerier); ok {
		refund, err := query.QueryRefund(ctx, order.ProviderOrderNo, number)
		if err == nil {
			switch refund.Status {
			case payment.PaymentRefunded:
				if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
					log.Printf("warning: mark refunded %s: %v", order.ID, err)
				}
				return
			case payment.PaymentRefundFailed:
				reason := firstNonEmptyText(refund.FailReason, "provider reported a refund failure")
				if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, truncateRunes(reason, 250)); err != nil && !errors.Is(err, domain.ErrConflict) {
					log.Printf("warning: mark refund failed %s: %v", order.ID, err)
				}
				return
			case payment.PaymentRefundPending:
				return
			}
		}
	}
	// The query could not confirm a refund (some providers return "not found"
	// when the original request never reached them). Re-submitting with the
	// same refund number is idempotent at both Alipay and WeChat.
	refund, err := gateway.Refund(ctx, payment.RefundRequest{
		OrderNo: order.ProviderOrderNo, RefundNo: number,
		TradeID: order.ProviderTradeID, AmountFen: order.AmountFen, Reason: "定时退款补偿",
	})
	if err != nil {
		log.Printf("warning: retry refund %s: %v", order.ID, err)
		return
	}
	switch refund.Status {
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: mark refunded %s: %v", order.ID, err)
		}
	case payment.PaymentRefundFailed:
		reason := firstNonEmptyText(refund.FailReason, "provider rejected the refund")
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, truncateRunes(reason, 250)); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: mark refund failed %s: %v", order.ID, err)
		}
	}
}

const (
	settlementReconcilePageSize = 200
	settlementReconcileMaxPages = 10
)

func (s *PaymentService) reconcile(ctx context.Context) {
	seen := 0
	for page := 0; page < settlementReconcileMaxPages; page++ {
		items, err := s.tasks.List(ctx, TaskStatusCompleted, repository.Page{
			Limit: settlementReconcilePageSize, Offset: page * settlementReconcilePageSize,
		})
		if err != nil {
			log.Printf("warning: settlement reconcile page %d: %v", page, err)
			return
		}
		for _, item := range items {
			if _, err := s.EnsureSettlement(ctx, item); err != nil {
				log.Printf("warning: reconcile settlement for task %s: %v", item.ID, err)
			}
		}
		seen += len(items)
		if len(items) < settlementReconcilePageSize {
			return
		}
	}
	log.Printf("warning: settlement reconcile stopped after %d completed tasks; older tasks need a bounded backfill", seen)
}
