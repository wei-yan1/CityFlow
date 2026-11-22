package taskservice

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cityflow/app/task/domain/task"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/testfixture/memory"
)

type fakeTrustedResolver struct {
	places map[string]location.Place
	err    error
	calls  int
}

func (f *fakeTrustedResolver) Resolve(_ context.Context, ids []string) ([]location.Place, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]location.Place, 0, len(ids))
	for i, id := range ids {
		place, ok := f.places[id]
		if !ok {
			return nil, fmt.Errorf("%w: 第 %d 个地点未验证或已过期，请重新搜索选择", domain.ErrInvalidRequest, i+1)
		}
		out = append(out, place)
	}
	return out, nil
}

func TestTaskUsesTrustedPlacesAndRejectsUnknown(t *testing.T) {
	ctx := context.Background()
	db, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	a := location.Place{ID: "a", Name: "真实商家", Address: "桂林", Location: location.Point{Latitude: 25.2, Longitude: 110.3}}
	b := location.Place{ID: "b", Name: "真实终点", Address: "桂林", Location: location.Point{Latitude: 25.3, Longitude: 110.4}}
	trusted := &fakeTrustedResolver{places: map[string]location.Place{a.ID: a, b.ID: b}}
	s := NewTaskService(db).WithTrustedPlaces(trusted, func(p location.Point) location.Point { return p })
	bad := &location.Point{Latitude: 39.9, Longitude: 116.4}
	req := task.CreateRequest{IdempotencyKey: "replay", Title: "test", Type: "DELIVERY", PickupStops: []task.PickupStop{{POIID: "a", Address: "伪造地址", Location: bad}, {POIID: "b", Address: "伪造"}}, DestinationPOIID: "b", Destination: "伪造", DestinationLocation: bad}
	created, err := s.Create(ctx, req, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if created.PickupStops[0].Address != "真实商家 · 桂林" || *created.PickupStops[0].Location != a.Location || created.Destination != "真实终点 · 桂林" || *created.DestinationLocation != b.Location {
		t.Fatalf("client data used: %+v", created)
	}
	trusted.err = errors.New("temporary failure")
	if replay, err := s.Create(ctx, req, "owner"); err != nil || replay.ID != created.ID {
		t.Fatalf("idempotent retry after resolver outage: %+v %v", replay, err)
	}
	trusted.err = nil
	req.IdempotencyKey = "new-request"
	req.PickupStops[1].POIID = "unknown"
	if _, err = s.Create(ctx, req, "owner"); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("unknown creation: %v", err)
	}
	update := task.UpdateRequest{Title: "updated", Type: "DELIVERY", PickupStops: req.PickupStops, DestinationPOIID: "b", Destination: "forged"}
	if _, err = s.Update(ctx, created.ID, update, "owner", "USER"); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("unknown update: %v", err)
	}
	update.PickupStops[1].POIID = "b"
	updated, err := s.Update(ctx, created.ID, update, "owner", "USER")
	if err != nil || updated.Destination != "真实终点 · 桂林" {
		t.Fatalf("update: %+v %v", updated, err)
	}
}

func TestTaskAcceptsExplicitTaskLocationsWithoutTrustedResolution(t *testing.T) {
	ctx := context.Background()
	db, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	trusted := &fakeTrustedResolver{err: errors.New("trusted lookup must not run")}
	s := NewTaskService(db).WithTrustedPlaces(trusted, func(p location.Point) location.Point { return p })
	pickup := &location.Point{Latitude: 25.281298, Longitude: 110.297326}
	destination := &location.Point{Latitude: 25.2835, Longitude: 110.328}
	req := task.CreateRequest{
		ID: "explicit-task", InitialStatus: task.StatusOpen, Title: "任务", Type: "DELIVERY",
		Pickup: "王城边边麻辣烫", Destination: "桂林电子科技大学",
		PickupLocation: pickup, DestinationLocation: destination,
	}
	created, err := s.Create(ctx, req, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "explicit-task" || created.Pickup != "王城边边麻辣烫" || created.Destination != "桂林电子科技大学" {
		t.Fatalf("explicit addresses lost: %+v", created)
	}
	if len(created.PickupStops) != 1 || created.PickupStops[0].Location == nil || *created.PickupStops[0].Location != *pickup {
		t.Fatalf("pickup stop was not synthesized: %+v", created.PickupStops)
	}
	if created.PickupLocation == nil || *created.PickupLocation != *pickup || created.DestinationLocation == nil || *created.DestinationLocation != *destination {
		t.Fatalf("explicit coordinates lost: %+v", created)
	}
	if trusted.calls != 0 {
		t.Fatalf("trusted resolver called %d times for explicit coordinates", trusted.calls)
	}
}
