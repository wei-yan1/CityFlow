package trackingservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/event"
	"cityflow/internal/testfixture/memory"
)

type failingTracks struct{}

func (failingTracks) RiderTrack(context.Context, string, time.Time, time.Time, uint64, int) ([]location.TrackPoint, error) {
	return nil, errors.New("redis down")
}

// fixtureViews projects the shared fixture's records into Tracking's local
// read views, mirroring the production RPC adapter.
type fixtureViews struct{ store *memory.Store }

func (f fixtureViews) Get(ctx context.Context, id string) (*TaskView, error) {
	item, err := f.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &TaskView{
		ID: item.ID, Status: item.Status, CreatedBy: item.CreatedBy, WorkerID: item.WorkerID,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	for _, event := range item.Events {
		out.Events = append(out.Events, TaskEvent{To: event.To, ActorID: event.ActorID, CreatedAt: event.CreatedAt})
	}
	return out, nil
}

func (f fixtureViews) RiderForUser(ctx context.Context, userID string) (*RiderView, error) {
	profile, err := f.store.RiderForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &RiderView{Status: profile.Status}, nil
}

func TestReportWritesRiderTrackAndHistoryUsesTaskWindow(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	riderID := "00000000-0000-4000-8000-000000000003"
	item, err := store.CreateQuotedTask(ctx, "track", "DELIVERY", "A", "B", "00000000-0000-4000-8000-000000000001", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim(ctx, item.ID, riderID); err != nil {
		t.Fatal(err)
	}
	positions := memory.NewPositions()
	events := event.NewPositions()
	ch, stop := events.Subscribe(riderID)
	defer stop()
	service := NewTrackingService(positions, events, fixtureViews{store: store}, fixtureViews{store: store}).WithTracks(positions)
	input := location.Report{
		TaskID:   item.ID,
		Point:    location.Point{Latitude: 31.23, Longitude: 121.47},
		Accuracy: 10, ObservedAt: time.Now().UTC(),
	}
	p, err := service.Report(ctx, riderID, input)
	if err != nil {
		t.Fatal(err)
	}
	track, err := positions.RiderTrack(ctx, riderID, time.Time{}, time.Time{}, 0, 100)
	if err != nil || len(track) != 1 || track[0].ID == 0 {
		t.Fatalf("rider track=%+v %v", track, err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("report not published")
	}
	claims := auth.Claims{Role: auth.RoleAdmin, Subject: "admin"}
	history, err := service.History(ctx, item.ID, claims, 0, 100)
	if err != nil || len(history) != 1 || history[0].TaskID != item.ID {
		t.Fatalf("history=%+v %v", history, err)
	}
	// Gateway fetches limit+1 to compute has_more; 501 must be accepted
	// internally even though the public REST limit stays capped at 500.
	if _, err := service.History(ctx, item.ID, claims, 0, 501); err != nil {
		t.Fatalf("gateway has_more fetch rejected: %v", err)
	}
	if _, err := service.History(ctx, item.ID, claims, 0, 502); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("oversized internal fetch should fail, got %v", err)
	}
	if history[0].ID != uint64(p.Version) {
		t.Fatalf("history id=%d want=%d", history[0].ID, p.Version)
	}
}

func TestHistoryForUnassignedTaskReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateQuotedTask(ctx, "open", "DELIVERY", "A", "B", "00000000-0000-4000-8000-000000000001", 0)
	if err != nil {
		t.Fatal(err)
	}
	service := NewTrackingService(memory.NewPositions(), event.NewPositions(), fixtureViews{store: store}, fixtureViews{store: store}).WithTracks(memory.NewPositions())
	for _, claims := range []auth.Claims{
		{Role: auth.RoleRider, Subject: "00000000-0000-4000-8000-000000000003"},
		{Role: auth.RoleUser, Subject: item.CreatedBy},
		{Role: auth.RoleAdmin, Subject: "admin"},
	} {
		history, err := service.History(ctx, item.ID, claims, 0, 501)
		if err != nil || len(history) != 0 {
			t.Fatalf("role=%s history=%+v err=%v", claims.Role, history, err)
		}
	}
}

func TestHistoryFailureDoesNotRejectLiveReport(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	riderID := "00000000-0000-4000-8000-000000000003"
	item, err := store.CreateQuotedTask(ctx, "track", "DELIVERY", "A", "B", "00000000-0000-4000-8000-000000000001", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim(ctx, item.ID, riderID); err != nil {
		t.Fatal(err)
	}
	events := event.NewPositions()
	positions := memory.NewPositions()
	service := NewTrackingService(positions, events, fixtureViews{store: store}, fixtureViews{store: store}).WithTracks(failingTracks{})
	input := location.Report{
		TaskID:   item.ID,
		Point:    location.Point{Latitude: 31.23, Longitude: 121.47},
		Accuracy: 10, ObservedAt: time.Now().UTC(),
	}
	if _, err := service.Report(ctx, riderID, input); err != nil {
		t.Fatalf("live report should not depend on history store: %v", err)
	}
	if _, err := service.History(ctx, item.ID, auth.Claims{Role: auth.RoleAdmin}, 0, 100); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("unexpected history error: %v", err)
	}
}
