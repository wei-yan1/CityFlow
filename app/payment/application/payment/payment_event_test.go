package paymentservice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cityflow/app/payment/domain/payment"
	"cityflow/app/payment/repository"
	"cityflow/internal/common/domain"
)

type eventInbox struct {
	status    map[string]string
	types     map[string]string
	payloads  map[string][]byte
	attempts  map[string]int
	updatedAt map[string]time.Time
	processed []string
	failed    []string
	retrying  []string
}

func newEventInbox() *eventInbox {
	return &eventInbox{
		status: map[string]string{}, types: map[string]string{}, payloads: map[string][]byte{},
		attempts: map[string]int{}, updatedAt: map[string]time.Time{},
	}
}

func (i *eventInbox) ClaimInbox(_ context.Context, eventID, eventType string, payload []byte) (bool, error) {
	switch i.status[eventID] {
	case "":
		i.status[eventID] = "PROCESSING"
		i.types[eventID] = eventType
		i.payloads[eventID] = append([]byte(nil), payload...)
		i.attempts[eventID] = 1
		i.updatedAt[eventID] = time.Now().UTC()
		return true, nil
	case "FAILED":
		i.status[eventID] = "PROCESSING"
		if i.types[eventID] == "" {
			i.types[eventID] = eventType
		}
		if len(i.payloads[eventID]) == 0 {
			i.payloads[eventID] = append([]byte(nil), payload...)
		}
		i.attempts[eventID]++
		i.updatedAt[eventID] = time.Now().UTC()
		return true, nil
	case "PROCESSING":
		if i.updatedAt[eventID].Before(time.Now().UTC().Add(-5 * time.Minute)) {
			i.attempts[eventID]++
			i.updatedAt[eventID] = time.Now().UTC()
			return true, nil
		}
		if len(i.payloads[eventID]) == 0 {
			i.payloads[eventID] = append([]byte(nil), payload...)
		}
		return false, nil
	default:
		return false, nil
	}
}

func (i *eventInbox) MarkInboxProcessed(_ context.Context, eventID string, _ time.Time) error {
	i.status[eventID] = "PROCESSED"
	i.processed = append(i.processed, eventID)
	return nil
}

func (i *eventInbox) MarkInboxFailed(_ context.Context, eventID, _ string) error {
	i.status[eventID] = "FAILED"
	i.failed = append(i.failed, eventID)
	return nil
}

func (i *eventInbox) MarkInboxRetrying(_ context.Context, eventID, _ string, at time.Time) error {
	i.status[eventID] = "PROCESSING"
	i.updatedAt[eventID] = at
	i.retrying = append(i.retrying, eventID)
	return nil
}

func (i *eventInbox) ClaimRecoverableInbox(_ context.Context, staleBefore time.Time, limit, maxAttempts int) ([]repository.InboxEvent, error) {
	events := make([]repository.InboxEvent, 0)
	for eventID, status := range i.status {
		if len(events) >= limit {
			break
		}
		if len(i.payloads[eventID]) == 0 || i.attempts[eventID] >= maxAttempts {
			continue
		}
		if status != "FAILED" && !(status == "PROCESSING" && i.updatedAt[eventID].Before(staleBefore)) {
			continue
		}
		i.status[eventID] = "PROCESSING"
		i.attempts[eventID]++
		i.updatedAt[eventID] = time.Now().UTC()
		events = append(events, repository.InboxEvent{
			EventID: eventID, EventType: i.types[eventID],
			Payload: append([]byte(nil), i.payloads[eventID]...), Attempts: i.attempts[eventID],
		})
	}
	return events, nil
}

func (i *eventInbox) InboxStatus(_ context.Context, eventID string) (string, error) {
	return i.status[eventID], nil
}

func (i *eventInbox) PurgeInbox(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func TestPaymentHandleCompletedEventCreatesSettlement(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	item := createQuotedTask(t, store, 1000)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkPaymentPaid(context.Background(), charge.Payment.ID, "MOCK-TRADE", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	item.WorkerID = "rider-1"
	item.Status = TaskStatusCompleted
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if err := payments.HandleTaskEvent(context.Background(), "evt-completed-1", "task.completed.v1", payload); err != nil {
		t.Fatal(err)
	}
	settlement, err := store.SettlementByTask(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("completed event did not create settlement: %v", err)
	}
	if settlement.RiderID != "rider-1" || settlement.AmountFen != 1000 {
		t.Fatalf("unexpected settlement: %+v", settlement)
	}
}

func TestPaymentInboxAcknowledgesDuplicateEvent(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	inbox := newEventInbox()
	payments.WithInbox(inbox)
	item := createQuotedTask(t, store, 500)
	item.Status = TaskStatusCompleted
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := payments.HandleTaskEvent(context.Background(), "evt-dup", "task.completed.v1", payload); err != nil {
			t.Fatalf("duplicate handling: %v", err)
		}
	}
	if len(inbox.processed) != 1 || len(inbox.failed) != 0 {
		t.Fatalf("duplicate event should be processed once: processed=%v failed=%v", inbox.processed, inbox.failed)
	}
}

func TestPaymentInboxMarksFailureForRetry(t *testing.T) {
	_, payments, _ := newPaymentTestService(t)
	inbox := newEventInbox()
	payments.WithInbox(inbox)
	// A malformed event fails before business work and must be retryable.
	err := payments.HandleTaskEvent(context.Background(), "evt-bad", "task.completed.v1", []byte("{"))
	if err == nil {
		t.Fatal("malformed event must fail")
	}
	if len(inbox.failed) != 1 {
		t.Fatalf("failed event was not recorded: %+v", inbox.failed)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("unexpected cancellation error")
	}
}

// A second delivery that arrives while the first is still PROCESSING must not
// be ACKed. Returning nil here would let RabbitMQ drop the event even though no
// settlement/refund exists yet.
func TestPaymentInboxDoesNotAcknowledgeFreshProcessingEvent(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	inbox := newEventInbox()
	payments.WithInbox(inbox)
	item := createQuotedTask(t, store, 500)
	item.Status = TaskStatusCompleted
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := inbox.ClaimInbox(context.Background(), "evt-in-flight", "task.completed.v1", payload)
	if err != nil || !claimed {
		t.Fatalf("simulate consumer claim: claimed=%v err=%v", claimed, err)
	}
	// The first consumer has claimed the row and would now crash; the broker
	// redelivers. The second delivery must return an error (no ACK).
	if err := payments.HandleTaskEvent(context.Background(), "evt-in-flight", "task.completed.v1", payload); err == nil {
		t.Fatal("fresh PROCESSING event must not be acknowledged")
	}
	if status := inbox.status["evt-in-flight"]; status != "PROCESSING" {
		t.Fatalf("unexpected inbox state: %s", status)
	}
	if _, err := store.SettlementByTask(context.Background(), item.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("settlement should not exist before recovery: %v", err)
	}
}

// Simulate a consumer that claimed the inbox row, crashed before creating the
// settlement, and never ACKed. After the lease expires, RecoverInbox must replay
// the persisted payload and complete the side effect.
func TestPaymentInboxRecoveryCompletesInterruptedSettlement(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	inbox := newEventInbox()
	payments.WithInbox(inbox)
	item := createQuotedTask(t, store, 700)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkPaymentPaid(context.Background(), charge.Payment.ID, "MOCK-RECOVER-1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	item.WorkerID = "rider-1"
	item.Status = TaskStatusCompleted
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := inbox.ClaimInbox(context.Background(), "evt-crash", "task.completed.v1", payload)
	if err != nil || !claimed {
		t.Fatalf("simulate consumer claim: claimed=%v err=%v", claimed, err)
	}
	// The consumer "crashes" here: no processTaskEvent, no MarkInboxProcessed.
	// Advance the row beyond the recovery lease.
	inbox.updatedAt["evt-crash"] = time.Now().UTC().Add(-10 * time.Minute)

	count, err := payments.RecoverInbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one recovered event, got %d", count)
	}
	settlement, err := store.SettlementByTask(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("recovery did not create settlement: %v", err)
	}
	if settlement.RiderID != "rider-1" || settlement.AmountFen != 700 {
		t.Fatalf("unexpected recovered settlement: %+v", settlement)
	}
	if status := inbox.status["evt-crash"]; status != "PROCESSED" {
		t.Fatalf("recovered event should be marked PROCESSED, got %s", status)
	}
}

func TestPaymentInboxRecoveryCompletesInterruptedRefund(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	inbox := newEventInbox()
	payments.WithInbox(inbox)
	item := createQuotedTask(t, store, 400)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkPaymentPaid(context.Background(), charge.Payment.ID, "MOCK-RECOVER-2", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	item.Status = TaskStatusCancelled
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := inbox.ClaimInbox(context.Background(), "evt-refund-crash", "task.cancelled.v1", payload)
	if err != nil || !claimed {
		t.Fatalf("simulate consumer claim: claimed=%v err=%v", claimed, err)
	}
	inbox.updatedAt["evt-refund-crash"] = time.Now().UTC().Add(-10 * time.Minute)

	count, err := payments.RecoverInbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one recovered event, got %d", count)
	}
	order, err := store.PaymentOrderByTask(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != payment.PaymentRefunded {
		t.Fatalf("recovery did not complete refund: status=%s", order.Status)
	}
	if status := inbox.status["evt-refund-crash"]; status != "PROCESSED" {
		t.Fatalf("recovered refund should be marked PROCESSED, got %s", status)
	}
}
