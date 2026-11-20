package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/task"
	"cityflow/internal/infrastructure/memory"
)

type trustedMemory struct {
	records map[string]repository.TrustedPlace
	err     error
	writes  int
}

func TestTaskUsesTrustedPlacesAndRejectsUnknown(t *testing.T) {
	ctx := context.Background()
	db, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	store := &trustedMemory{records: map[string]repository.TrustedPlace{}}
	trusted := TrustedPlaces{Store: store, Lifetime: time.Minute}
	a := location.Place{ID: "a", Name: "真实商家", Address: "桂林", Location: location.Point{Latitude: 25.2, Longitude: 110.3}}
	b := location.Place{ID: "b", Name: "真实终点", Address: "桂林", Location: location.Point{Latitude: 25.3, Longitude: 110.4}}
	if err = trusted.Record(ctx, []location.Place{a, b}); err != nil {
		t.Fatal(err)
	}
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
	store.err = errors.New("temporary failure")
	if replay, err := s.Create(ctx, req, "owner"); err != nil || replay.ID != created.ID {
		t.Fatalf("idempotent retry after store outage: %+v %v", replay, err)
	}
	store.err = nil
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

func (m *trustedMemory) SaveTrustedPlaces(_ context.Context, p []repository.TrustedPlace) error {
	if m.err != nil {
		return m.err
	}
	m.writes++
	for _, v := range p {
		m.records[v.Place.ID] = v
	}
	return nil
}

func TestTrustedPlaceReuseReturnsStoredData(t *testing.T) {
	store := &trustedMemory{records: map[string]repository.TrustedPlace{}}
	s := TrustedPlaces{Store: store, Lifetime: time.Minute}
	ctx := context.Background()
	first := location.Place{ID: "one", Name: "原名称", Location: location.Point{Latitude: 25, Longitude: 110}}
	if _, err := s.RecordAndGet(ctx, []location.Place{first}); err != nil {
		t.Fatal(err)
	}
	stored := store.records[first.ID]
	changed := first
	changed.Name = "搜索新名称"
	result, err := s.RecordAndGet(ctx, []location.Place{changed})
	if err != nil || len(result) != 1 || result[0].Name != first.Name || store.writes != 1 || !store.records[first.ID].ExpiresAt.Equal(stored.ExpiresAt) {
		t.Fatalf("reuse: %+v writes=%d err=%v", result, store.writes, err)
	}
}
func (m *trustedMemory) FindTrustedPlaces(_ context.Context, ids []string, now time.Time) (map[string]repository.TrustedPlace, error) {
	if m.err != nil {
		return nil, m.err
	}
	r := map[string]repository.TrustedPlace{}
	for _, id := range ids {
		if p, ok := m.records[id]; ok && p.ExpiresAt.After(now) {
			r[id] = p
		}
	}
	return r, nil
}
func TestTrustedPlacesRequireProviderRecordsAndExpiry(t *testing.T) {
	store := &trustedMemory{records: map[string]repository.TrustedPlace{}}
	service := TrustedPlaces{Store: store, Lifetime: time.Minute}
	ctx := context.Background()
	p := location.Place{ID: "guilin-poi", Name: "地点", Address: "桂林", Location: location.Point{Latitude: 25.2, Longitude: 110.3}}
	if _, err := service.Resolve(ctx, []string{p.ID, "invented"}); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("unknown accepted: %v", err)
	}
	if err := service.Record(ctx, []location.Place{p, {ID: "", Name: "missing id"}}); err != nil {
		t.Fatal(err)
	}
	got, err := service.Resolve(ctx, []string{p.ID, p.ID})
	if err != nil || len(got) != 2 || got[0] != p {
		t.Fatalf("provider result: %+v %v", got, err)
	}
	old := store.records[p.ID]
	old.ExpiresAt = time.Now().Add(-time.Second)
	store.records[p.ID] = old
	if _, err = service.Resolve(ctx, []string{p.ID, p.ID}); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("expired accepted: %v", err)
	}
	store.err = errors.New("offline")
	if _, err = service.Resolve(ctx, []string{p.ID, p.ID}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("failure must be closed: %v", err)
	}
}
