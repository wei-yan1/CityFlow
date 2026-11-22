package paymentservice

import (
	"context"
	"errors"
	"time"

	"cityflow/app/payment/repository"
)

const (
	notifyPurgeBatch   = 1000
	maintenanceMaxPass = 5
)

// ReconcileJob is the scheduler entry for the singleton reconciliation scan.
// It performs the same bounded work as the local worker: settle recent
// completions, query pending payments/refunds, and backfill missing settlements.
func (s *PaymentService) ReconcileJob(ctx context.Context) (int, int) {
	s.reconcile(ctx)
	return s.ReconcileOnce(ctx)
}

// PurgeInboxJob deletes processed inbox rows older than the retention window.
// The loop is bounded so a scheduler invocation cannot run unboundedly after
// a long outage.
func (s *PaymentService) PurgeInboxJob(ctx context.Context) (int64, error) {
	if s == nil || s.inbox == nil {
		return 0, nil
	}
	before := s.now().Add(-inboxRetention)
	var total int64
	for pass := 0; pass < maintenanceMaxPass; pass++ {
		deleted, err := s.inbox.PurgeInbox(ctx, before, inboxPurgeBatch)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < int64(inboxPurgeBatch) {
			break
		}
	}
	return total, nil
}

// PurgeNotifyLogsJob deletes provider callback audit rows older than the
// configured compliance retention. It intentionally requires an explicit
// retention instead of reusing the inbox's 30-day broker replay window.
func (s *PaymentService) PurgeNotifyLogsJob(ctx context.Context, retention time.Duration, batch, maxPasses int) (int64, error) {
	if s == nil || s.orders == nil {
		return 0, errors.New("payment store is unavailable")
	}
	if retention <= 0 {
		return 0, errors.New("payment notify retention must be positive")
	}
	purger, ok := s.orders.(repository.PaymentNotifyLogPurger)
	if !ok {
		return 0, errors.New("payment store does not support notify log purge")
	}
	if batch <= 0 {
		batch = notifyPurgeBatch
	}
	if maxPasses <= 0 {
		maxPasses = maintenanceMaxPass
	}
	before := s.now().Add(-retention)
	var total int64
	for pass := 0; pass < maxPasses; pass++ {
		deleted, err := purger.PurgeNotifies(ctx, before, batch)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < int64(batch) {
			break
		}
	}
	return total, nil
}
