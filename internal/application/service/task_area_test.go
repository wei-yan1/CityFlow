package service

import (
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/task"
	"cityflow/internal/infrastructure/memory"
	"context"
	"errors"
	"testing"
	"time"
)

type areaStub struct {
	calls int
	err   error
}

func (a *areaStub) Validate(_ context.Context, _ []location.Point) error { a.calls++; return a.err }
func TestTaskAreaEnforcementAndIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	area := &areaStub{}
	service := NewTaskService(store).WithServiceArea(area)
	p := &location.Point{Latitude: 25.27, Longitude: 110.29}
	request := task.CreateRequest{Title: "test", Type: "DELIVERY", Pickup: "A", Destination: "B", PickupLocation: p, DestinationLocation: p, IdempotencyKey: "test"}
	first, err := service.Create(ctx, request, "owner")
	if err != nil {
		t.Fatal(err)
	}
	area.err = domain.ErrUnavailable
	again, err := service.Create(ctx, request, "owner")
	if err != nil || again.ID != first.ID || area.calls != 1 {
		t.Fatalf("replay: %+v %v calls=%d", again, err, area.calls)
	}
	request.IdempotencyKey = "new"
	if _, err = service.Create(ctx, request, "owner"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("outage: %v", err)
	}
	request.DestinationLocation = nil
	if _, err = service.Create(ctx, request, "owner"); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("missing coords: %v", err)
	}
}

func TestLegacyTaskCanEditTextButChangedLocationIsValidated(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := store.Create(ctx, task.CreateRequest{Title: "old", Type: "DELIVERY", Pickup: "A", Destination: "B"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	area := &areaStub{err: domain.ErrUnavailable}
	service := NewTaskService(store).WithServiceArea(area)
	update := task.UpdateRequest{Title: "new", Type: "DELIVERY", Pickup: "A", Destination: "B"}
	if _, err = service.Update(ctx, legacy.ID, update, "owner", "USER"); err != nil {
		t.Fatal(err)
	}
	update.Destination = "C"
	if _, err = service.Update(ctx, legacy.ID, update, "owner", "USER"); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("missing new coordinate: %v", err)
	}
}

func TestOnlyLocationChangesRequireAreaLookup(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	p := &location.Point{Latitude: 25.27, Longitude: 110.29}
	existing, err := store.Create(ctx, task.CreateRequest{Title: "old", Type: "DELIVERY", Pickup: "A", Destination: "B", PickupStops: []task.PickupStop{{Address: "A", Location: p}}, PickupLocation: p, DestinationLocation: p}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	area := &areaStub{err: domain.ErrUnavailable}
	service := NewTaskService(store).WithServiceArea(area)
	at := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	update := task.UpdateRequest{Title: "old", Type: "DELIVERY", Pickup: "A", Destination: "B", PickupStops: []task.PickupStop{{Address: "A", Location: p, PickupAt: &at}}, PickupLocation: p, DestinationLocation: p}
	if _, err = service.Update(ctx, existing.ID, update, "owner", "USER"); err != nil || area.calls != 0 {
		t.Fatalf("schedule-only: %v calls=%d", err, area.calls)
	}
	update.PickupStops[0].Location = &location.Point{Latitude: 25.28, Longitude: 110.29}
	if _, err = service.Update(ctx, existing.ID, update, "owner", "USER"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("changed point: %v", err)
	}
}
