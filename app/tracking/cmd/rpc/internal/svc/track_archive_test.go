package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	trackingservice "cityflow/app/tracking/application/tracking"
	"cityflow/app/tracking/repository"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/messaging"
)

type archiveRecorder struct {
	events *[]string
	points []location.TrackPoint
	err    error
}

func (a *archiveRecorder) ArchiveTrackPoints(_ context.Context, points []location.TrackPoint) error {
	*a.events = append(*a.events, "archive")
	if a.err != nil {
		return a.err
	}
	a.points = append(a.points, points...)
	return nil
}

func (a *archiveRecorder) RiderTrack(context.Context, string, time.Time, time.Time, uint64, int) ([]location.TrackPoint, error) {
	return nil, nil
}

type queueRecorder struct {
	events  *[]string
	removed []string
	forgot  []string
}

func (q *queueRecorder) PendingRiders(context.Context) ([]string, error) { return nil, nil }
func (q *queueRecorder) BackfillPendingRiders(context.Context) error     { return nil }
func (q *queueRecorder) PendingTrackBatch(context.Context, string, uint64, int) (repository.TrackBatch, error) {
	return repository.TrackBatch{}, nil
}
func (q *queueRecorder) TrackCursor(context.Context, string) (uint64, error) { return 0, nil }
func (q *queueRecorder) SetTrackCursor(context.Context, string, uint64) error {
	return nil
}
func (q *queueRecorder) RemoveTrackMembers(_ context.Context, _ string, members []string) error {
	*q.events = append(*q.events, "remove")
	q.removed = append(q.removed, members...)
	return nil
}
func (q *queueRecorder) ForgetRiderIfEmpty(_ context.Context, riderID string) (bool, error) {
	*q.events = append(*q.events, "forget")
	q.forgot = append(q.forgot, riderID)
	return true, nil
}
func (q *queueRecorder) AcquireArchiverLock(context.Context, time.Duration) (string, bool, error) {
	return "", false, nil
}
func (q *queueRecorder) ReleaseArchiverLock(context.Context, string) error { return nil }

func trackBatchMessage(t *testing.T, riderID string) messaging.Message {
	t.Helper()
	body, err := json.Marshal(trackingservice.TrackBatchPayload{
		RiderID: riderID,
		Points: []trackingservice.TrackBatchPoint{
			{Member: `{"version":1}`, Position: location.Position{RiderID: riderID, Version: 1, ObservedAt: time.Now().UTC()}},
			{Member: `{"version":2}`, Position: location.Position{RiderID: riderID, Version: 2, ObservedAt: time.Now().UTC()}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return messaging.Message{Type: trackingservice.TrackArchiveTopic, Payload: body}
}

func TestHandleTrackBatchArchivesBeforeRemovingRedis(t *testing.T) {
	var events []string
	archive := &archiveRecorder{events: &events}
	queue := &queueRecorder{events: &events}
	ctx := &ServiceContext{TrackArchive: archive, TrackQueue: queue}
	if err := ctx.handleTrackBatch(context.Background(), trackBatchMessage(t, "rider-1")); err != nil {
		t.Fatal(err)
	}
	if len(archive.points) != 2 {
		t.Fatalf("archived %d points, want 2", len(archive.points))
	}
	if len(queue.removed) != 2 || queue.removed[0] != `{"version":1}` || queue.removed[1] != `{"version":2}` {
		t.Fatalf("removed members=%v", queue.removed)
	}
	if len(queue.forgot) != 1 || queue.forgot[0] != "rider-1" {
		t.Fatalf("forgot riders=%v", queue.forgot)
	}
	want := []string{"archive", "remove", "forget"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("events=%v want=%v", events, want)
	}
}

func TestHandleTrackBatchKeepsRedisWhenArchiveFails(t *testing.T) {
	var events []string
	archive := &archiveRecorder{events: &events, err: errors.New("mysql down")}
	queue := &queueRecorder{events: &events}
	ctx := &ServiceContext{TrackArchive: archive, TrackQueue: queue}
	if err := ctx.handleTrackBatch(context.Background(), trackBatchMessage(t, "rider-1")); err == nil {
		t.Fatal("batch with failed MySQL archive should return an error for retry")
	}
	if len(queue.removed) != 0 || len(queue.forgot) != 0 {
		t.Fatalf("Redis was modified before MySQL commit: removed=%v forgot=%v", queue.removed, queue.forgot)
	}
}

func TestHandleTrackBatchRejectsEmptyMember(t *testing.T) {
	var events []string
	archive := &archiveRecorder{events: &events}
	queue := &queueRecorder{events: &events}
	ctx := &ServiceContext{TrackArchive: archive, TrackQueue: queue}
	body, err := json.Marshal(trackingservice.TrackBatchPayload{
		RiderID: "rider-1",
		Points:  []trackingservice.TrackBatchPoint{{Position: location.Position{RiderID: "rider-1", Version: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.handleTrackBatch(context.Background(), messaging.Message{Type: trackingservice.TrackArchiveTopic, Payload: body}); err == nil {
		t.Fatal("batch with an empty Redis member should fail")
	}
	if len(events) != 0 {
		t.Fatalf("invalid batch reached stores: events=%v", events)
	}
}
