package reaction

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cityflow/internal/common/domain"
	"cityflow/internal/platform/messaging"
)

type fakeStore struct {
	pending   []Pending
	requeued  []Pending
	popErr    error
	requeueFn func([]Pending) error
}

func (f *fakeStore) Set(context.Context, string, string, string, string, bool, time.Time) (bool, int64, error) {
	return false, 0, nil
}

func (f *fakeStore) States(context.Context, string, []string, string, string) ([]State, error) {
	return nil, nil
}

func (f *fakeStore) ListMine(context.Context, string, string, string, string, int) ([]Mine, string, error) {
	return nil, "", nil
}

func (f *fakeStore) PopPending(context.Context, string, int64) ([]Pending, error) {
	return f.pending, f.popErr
}

func (f *fakeStore) RequeuePending(_ context.Context, _ string, items []Pending) error {
	f.requeued = append(f.requeued, items...)
	if f.requeueFn != nil {
		return f.requeueFn(items)
	}
	return nil
}

func (f *fakeStore) Ping(context.Context) error { return nil }

type recordingPublisher struct {
	messages []messaging.Message
	err      error
}

func (p *recordingPublisher) Publish(_ context.Context, msg messaging.Message) error {
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, msg)
	return nil
}

func TestProjectOncePublishesAbsoluteCounts(t *testing.T) {
	store := &fakeStore{pending: []Pending{{TargetID: "101", Count: 12}, {TargetID: "205", Count: 3}}}
	publisher := &recordingPublisher{}
	svc := New(store)
	svc.projectOnce(context.Background(), "PLACE_REVIEW", publisher, 30)

	if len(publisher.messages) != 1 {
		t.Fatalf("messages = %d", len(publisher.messages))
	}
	msg := publisher.messages[0]
	if msg.Type != "PLACE_REVIEW.times.changed" {
		t.Fatalf("type = %q", msg.Type)
	}
	var items []projectionItem
	if err := json.Unmarshal(msg.Payload, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].BizID != "101" || items[0].LikedTimes != 12 || items[1].LikedTimes != 3 {
		t.Fatalf("payload = %s", msg.Payload)
	}
	if len(store.requeued) != 0 {
		t.Fatalf("requeued on success = %+v", store.requeued)
	}
}

func TestProjectOnceRequeuesFailedBatch(t *testing.T) {
	store := &fakeStore{pending: []Pending{{TargetID: "101", Count: 12}}}
	publisher := &recordingPublisher{err: errors.New("rabbitmq down")}
	svc := New(store)
	svc.projectOnce(context.Background(), "PLACE_COMMENT", publisher, 30)

	if len(publisher.messages) != 0 {
		t.Fatalf("published after failure = %+v", publisher.messages)
	}
	if len(store.requeued) != 1 || store.requeued[0].TargetID != "101" || store.requeued[0].Count != 12 {
		t.Fatalf("requeued = %+v", store.requeued)
	}
}

func TestSetRejectsUnknownTargetAndAnonymousUser(t *testing.T) {
	svc := New(&fakeStore{})
	if _, err := svc.Set(context.Background(), "UNKNOWN", "101", "user-1", "LIKE", true); err == nil {
		t.Fatal("expected invalid target type")
	}
	if _, err := svc.Set(context.Background(), "PLACE_REVIEW", "101", " ", "LIKE", true); err == nil {
		t.Fatal("expected unauthorized user")
	}
}

func TestSetAcceptsContentTargetTypesOnly(t *testing.T) {
	svc := New(&fakeStore{})
	for _, targetType := range []string{"PLACE", "PLACE_REVIEW", "PLACE_COMMENT", "RIDER_REVIEW", "RIDER_COMMENT"} {
		if _, err := svc.Set(context.Background(), targetType, "101", "user-1", "LIKE", true); err != nil {
			t.Errorf("%s rejected: %v", targetType, err)
		}
	}
	if _, err := svc.Set(context.Background(), "MERCHANT", "101", "user-1", "LIKE", true); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("merchant must be owned by Discovery, got %v", err)
	}
}
