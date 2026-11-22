package trackingservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"cityflow/internal/common/location"
)

type stubTrackReader struct {
	points    []location.TrackPoint
	err       error
	afterSeen *uint64
}

func (s stubTrackReader) RiderTrack(_ context.Context, _ string, _, _ time.Time, after uint64, _ int) ([]location.TrackPoint, error) {
	if s.afterSeen != nil {
		*s.afterSeen = after
	}
	return s.points, s.err
}

func TestMergedTrackReaderCombinesArchiveAndPending(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	archived := []location.TrackPoint{
		{ID: 1, TaskID: "archive", Position: location.Position{ObservedAt: base}},
		{ID: 2, TaskID: "archive", Position: location.Position{ObservedAt: base.Add(time.Minute)}},
	}
	pending := []location.TrackPoint{
		{ID: 2, TaskID: "pending", Position: location.Position{ObservedAt: base.Add(time.Minute)}},
		{ID: 3, TaskID: "pending", Position: location.Position{ObservedAt: base.Add(2 * time.Minute)}},
		{ID: 4, TaskID: "pending", Position: location.Position{ObservedAt: base.Add(3 * time.Minute)}},
	}
	pendingAfter := uint64(999)
	reader := NewMergedTrackReader(
		stubTrackReader{points: archived},
		stubTrackReader{points: pending, afterSeen: &pendingAfter},
	)
	got, err := reader.RiderTrack(ctx, "rider", time.Time{}, base.Add(10*time.Minute), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if pendingAfter != 0 {
		t.Fatalf("pending reader after=%d, want 0 so uncommitted points are not skipped", pendingAfter)
	}
	if len(got) != 3 || got[0].ID != 2 || got[1].ID != 3 || got[2].ID != 4 {
		t.Fatalf("merged points=%+v", got)
	}
	if got[0].TaskID != "archive" {
		t.Fatalf("duplicate point used %q copy, want archive", got[0].TaskID)
	}
}

func TestMergedTrackReaderAppliesCursorAfterMerge(t *testing.T) {
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	reader := NewMergedTrackReader(nil, stubTrackReader{points: []location.TrackPoint{
		{ID: 1, Position: location.Position{ObservedAt: base}},
		{ID: 2, Position: location.Position{ObservedAt: base.Add(time.Minute)}},
		{ID: 3, Position: location.Position{ObservedAt: base.Add(2 * time.Minute)}},
	}})
	got, err := reader.RiderTrack(context.Background(), "rider", time.Time{}, time.Time{}, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("points after cursor=%+v, want only ID 3", got)
	}
}

func TestMergedTrackReaderSortsAndLimits(t *testing.T) {
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	reader := NewMergedTrackReader(nil, stubTrackReader{points: []location.TrackPoint{
		{ID: 10, Position: location.Position{ObservedAt: base.Add(2 * time.Minute)}},
		{ID: 20, Position: location.Position{ObservedAt: base}},
		{ID: 30, Position: location.Position{ObservedAt: base.Add(time.Minute)}},
	}})
	got, err := reader.RiderTrack(context.Background(), "rider", time.Time{}, time.Time{}, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 20 || got[1].ID != 30 {
		t.Fatalf("sorted limited points=%+v", got)
	}
}

func TestMergedTrackReaderFallsBackWhenOneStoreFails(t *testing.T) {
	base := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	points := []location.TrackPoint{{ID: 1, Position: location.Position{ObservedAt: base}}}
	ctx := context.Background()

	archiveDown := NewMergedTrackReader(stubTrackReader{err: errors.New("mysql down")}, stubTrackReader{points: points})
	got, err := archiveDown.RiderTrack(ctx, "rider", time.Time{}, time.Time{}, 0, 10)
	if err != nil || len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("archive fallback points=%+v err=%v", got, err)
	}

	pendingDown := NewMergedTrackReader(stubTrackReader{points: points}, stubTrackReader{err: errors.New("redis down")})
	got, err = pendingDown.RiderTrack(ctx, "rider", time.Time{}, time.Time{}, 0, 10)
	if err != nil || len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("pending fallback points=%+v err=%v", got, err)
	}

	bothDown := NewMergedTrackReader(stubTrackReader{err: errors.New("mysql down")}, stubTrackReader{err: errors.New("redis down")})
	if _, err := bothDown.RiderTrack(ctx, "rider", time.Time{}, time.Time{}, 0, 10); err == nil {
		t.Fatal("both stores down should return an error")
	}
}
