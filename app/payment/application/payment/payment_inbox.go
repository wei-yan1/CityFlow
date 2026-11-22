package paymentservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"cityflow/app/payment/domain/payment"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
)

// HandleTaskEvent is the Payment consumer's boundary. It is idempotent via the
// inbox (event ID) and again via settlement/refund business keys. Returning nil
// acknowledges the event; returning an error asks the transport to retry.
func (s *PaymentService) HandleTaskEvent(ctx context.Context, eventID, eventType string, payload []byte) error {
	if s.inbox != nil {
		claimed, err := s.inbox.ClaimInbox(ctx, eventID, eventType, payload)
		if err != nil {
			return fmt.Errorf("claim payment inbox: %w", err)
		}
		if !claimed {
			status, statusErr := s.inbox.InboxStatus(ctx, eventID)
			if statusErr != nil {
				return fmt.Errorf("payment inbox status %s: %w", eventID, statusErr)
			}
			switch status {
			case "PROCESSED":
				log.Printf("payment inbox: duplicate event %s (%s) already processed, acknowledged", eventID, eventType)
				return nil
			case "PROCESSING":
				// ClaimInbox returns false while another delivery still owns the
				// lease. ACKing here would silently drop the event for the whole
				// lease window if that owner crashes, so ask the transport to retry
				// instead. The payload is already durable and RecoverInbox replays
				// stale PROCESSING rows if retries are exhausted.
				return fmt.Errorf("payment inbox event %s is still PROCESSING; retry later", eventID)
			default:
				return fmt.Errorf("payment inbox event %s has unexpected state %q; retry", eventID, status)
			}
		}
	}
	if err := s.processTaskEvent(ctx, eventType, payload); err != nil {
		s.markInboxFailed(ctx, eventID, err)
		return err
	}
	if s.inbox != nil {
		if markErr := s.inbox.MarkInboxProcessed(ctx, eventID, s.now()); markErr != nil {
			return fmt.Errorf("mark payment inbox processed: %w", markErr)
		}
	}
	return nil
}

// processTaskEvent applies the event's business side effect without touching
// the inbox. It is shared by the live consumer and the Inbox recovery worker.
func (s *PaymentService) processTaskEvent(ctx context.Context, eventType string, payload []byte) error {
	var item TaskView
	if err := json.Unmarshal(payload, &item); err != nil {
		return fmt.Errorf("decode %s payload: %w", eventType, err)
	}
	switch eventType {
	case "task.completed.v1":
		if item.ID == "" || item.Status != TaskStatusCompleted {
			return fmt.Errorf("%w: completed event has no completed task", domain.ErrInvalidRequest)
		}
		_, err := s.EnsureSettlement(ctx, &item)
		return err
	case "task.cancelled.v1":
		if item.ID == "" || item.Status != TaskStatusCancelled {
			return fmt.Errorf("%w: cancelled event has no cancelled task", domain.ErrInvalidRequest)
		}
		return s.RefundCancelledTask(ctx, &item)
	default:
		log.Printf("payment consumer: ignoring unsupported event %s", eventType)
		return nil
	}
}

// RecoverInbox replays events whose consumer crashed after claiming the inbox
// row but before finishing the settlement/refund side effect. It only leases
// rows after the PROCESSING lease expires, so a live consumer is not raced.
func (s *PaymentService) RecoverInbox(ctx context.Context) (int, error) {
	if s.inbox == nil {
		return 0, nil
	}
	events, err := s.inbox.ClaimRecoverableInbox(ctx, s.now().Add(-inboxRecoveryLease), inboxRecoveryBatch, inboxMaxAttempts)
	if err != nil {
		return 0, fmt.Errorf("claim recoverable payment inbox: %w", err)
	}
	recovered := 0
	for _, event := range events {
		if err := s.processTaskEvent(ctx, event.EventType, event.Payload); err != nil {
			if event.Attempts >= inboxMaxAttempts {
				s.markInboxFailed(ctx, event.EventID, err)
			} else if retryErr := s.inbox.MarkInboxRetrying(ctx, event.EventID, truncateRunes(err.Error(), 250), s.now()); retryErr != nil {
				log.Printf("warning: reschedule payment inbox %s: %v", event.EventID, retryErr)
			}
			continue
		}
		if markErr := s.inbox.MarkInboxProcessed(ctx, event.EventID, s.now()); markErr != nil {
			log.Printf("warning: mark recovered payment inbox %s processed: %v", event.EventID, markErr)
			continue
		}
		recovered++
	}
	return recovered, nil
}

// RunInboxRecoveryWorker periodically replays stale inbox rows. Multiple
// Payment instances may run it because ClaimRecoverableInbox uses a lease and
// FOR UPDATE SKIP LOCKED.
func (s *PaymentService) RunInboxRecoveryWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = inboxRecoveryInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if count, err := s.RecoverInbox(ctx); err != nil {
		log.Printf("warning: inbox recovery worker: %v", err)
	} else if count > 0 {
		log.Printf("payment inbox recovery: replayed %d events", count)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if count, err := s.RecoverInbox(ctx); err != nil {
				log.Printf("warning: inbox recovery worker: %v", err)
			} else if count > 0 {
				log.Printf("payment inbox recovery: replayed %d events", count)
			}
		}
	}
}

func (s *PaymentService) markInboxFailed(ctx context.Context, eventID string, cause error) {
	if s.inbox == nil || eventID == "" {
		return
	}
	if err := s.inbox.MarkInboxFailed(ctx, eventID, truncateRunes(cause.Error(), 250)); err != nil {
		log.Printf("warning: mark payment inbox failed %s: %v", eventID, err)
	}
}

// RefundCancelledTask returns nil when there is no paid order to refund; that
// is a normal outcome for an unpaid task and must not be retried forever.
func (s *PaymentService) RefundCancelledTask(ctx context.Context, item *TaskView) error {
	if item == nil || item.ID == "" {
		return fmt.Errorf("%w: cancelled event has no task id", domain.ErrInvalidRequest)
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if order.Status == payment.PaymentRefunded {
		return nil
	}
	if order.Status != payment.PaymentPaid && order.Status != payment.PaymentRefundPending && order.Status != payment.PaymentRefundFailed {
		return nil
	}
	actorID := item.CreatedBy
	if actorID == "" {
		actorID = order.PayerID
	}
	if _, err := s.Refund(ctx, order.ID, actorID, auth.RoleUser, firstNonEmptyText(item.CancelReason, "任务取消")); err != nil {
		return fmt.Errorf("refund cancelled task %s: %w", item.ID, err)
	}
	return nil
}
