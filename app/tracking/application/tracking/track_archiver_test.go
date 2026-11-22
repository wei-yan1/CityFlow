package trackingservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"cityflow/app/tracking/repository"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/messaging"
)

type fakeTrackQueue struct {
	riders    []string
	points    map[string][]location.TrackPoint
	cursor    map[string]uint64
	lock      bool
	published int
	removed   []string
}

func newFakeTrackQueue(riders []string, points map[string][]location.TrackPoint) *fakeTrackQueue {
	return &fakeTrackQueue{riders: riders, points: points, cursor: map[string]uint64{}, lock: true}
}

func (q *fakeTrackQueue) PendingRiders(context.Context) ([]string, error) {
	return append([]string(nil), q.riders...), nil
}

func (q *fakeTrackQueue) BackfillPendingRiders(context.Context) error { return nil }

func (q *fakeTrackQueue) PendingTrackBatch(_ context.Context, riderID string, after uint64, limit int) (repository.TrackBatch, error) {
	batch := repository.TrackBatch{RiderID: riderID}
	for _, point := range q.points[riderID] {
		if point.ID <= after {
			continue
		}
		batch.Points = append(batch.Points, point)
		batch.Members = append(batch.Members, fmt.Sprintf("member-%d", point.ID))
		if point.ID > batch.LastVersion {
			batch.LastVersion = point.ID
		}
		if len(batch.Points) == limit {
			break
		}
	}
	return batch, nil
}

func (q *fakeTrackQueue) TrackCursor(_ context.Context, riderID string) (uint64, error) {
	return q.cursor[riderID], nil
}

func (q *fakeTrackQueue) SetTrackCursor(_ context.Context, riderID string, version uint64) error {
	q.cursor[riderID] = version
	return nil
}

func (q *fakeTrackQueue) RemoveTrackMembers(_ context.Context, _ string, members []string) error {
	q.removed = append(q.removed, members...)
	return nil
}

func (q *fakeTrackQueue) ForgetRiderIfEmpty(context.Context, string) (bool, error) {
	return false, nil
}

func (q *fakeTrackQueue) AcquireArchiverLock(context.Context, time.Duration) (string, bool, error) {
	if !q.lock {
		return "", false, nil
	}
	return "token", true, nil
}

func (q *fakeTrackQueue) ReleaseArchiverLock(context.Context, string) error {
	return nil
}

type fakePublisher struct {
	messages []messaging.Message
	err      error
}

func (p *fakePublisher) Publish(_ context.Context, msg messaging.Message) error {
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, msg)
	return nil
}

func (p *fakePublisher) Close() error { return nil }

func TestTrackArchiverPublishesAndAdvancesCursor(t *testing.T) {
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	queue := newFakeTrackQueue([]string{"rider"}, map[string][]location.TrackPoint{
		"rider": {
			{ID: 5, Position: location.Position{RiderID: "rider", ObservedAt: base, UpdatedAt: base}},
		},
	})
	publisher := &fakePublisher{}
	archiver := NewTrackArchiver(queue, publisher)
	if err := archiver.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("published %d messages, want 1", len(publisher.messages))
	}
	if got := queue.cursor["rider"]; got != 5 {
		t.Fatalf("cursor=%d want 5", got)
	}
	var payload TrackBatchPayload
	if err := json.Unmarshal(publisher.messages[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RiderID != "rider" || len(payload.Points) != 1 || payload.Points[0].Member != "member-5" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestTrackArchiverDoesNotAdvanceCursorWhenPublishFails(t *testing.T) {
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	queue := newFakeTrackQueue([]string{"rider"}, map[string][]location.TrackPoint{
		"rider": {{ID: 7, Position: location.Position{RiderID: "rider", ObservedAt: base, UpdatedAt: base}}},
	})
	publisher := &fakePublisher{err: errors.New("broker down")}
	archiver := NewTrackArchiver(queue, publisher)
	if err := archiver.flush(context.Background()); err == nil {
		t.Fatal("flush should fail when publish fails")
	}
	if got := queue.cursor["rider"]; got != 0 {
		t.Fatalf("cursor=%d, want 0 after publish failure", got)
	}
}

func TestTrackArchiverRewindsCursorForStalePendingPoint(t *testing.T) {
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	queue := newFakeTrackQueue([]string{"rider"}, map[string][]location.TrackPoint{
		"rider": {{ID: 10, Position: location.Position{RiderID: "rider", ObservedAt: base.Add(-3 * time.Minute), UpdatedAt: base.Add(-3 * time.Minute)}}},
	})
	queue.cursor["rider"] = 10
	publisher := &fakePublisher{}
	archiver := NewTrackArchiver(queue, publisher)
	if err := archiver.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("published %d messages, want 1 after cursor rewind", len(publisher.messages))
	}
	if got := queue.cursor["rider"]; got != 10 {
		t.Fatalf("cursor=%d want 10 after republishing", got)
	}
	// A second flush must not immediately republish the same stale batch; a
	// consumer outage would otherwise amplify queue traffic every tick.
	if err := archiver.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("published %d messages, want 1 after rewind throttle", len(publisher.messages))
	}
}

func TestTrackArchiverSkipsWhenLockIsHeld(t *testing.T) {
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	queue := newFakeTrackQueue([]string{"rider"}, map[string][]location.TrackPoint{
		"rider": {{ID: 1, Position: location.Position{RiderID: "rider", ObservedAt: base, UpdatedAt: base}}},
	})
	queue.lock = false
	publisher := &fakePublisher{}
	archiver := NewTrackArchiver(queue, publisher)
	if err := archiver.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("published %d messages while another archiver holds the lock", len(publisher.messages))
	}
}
