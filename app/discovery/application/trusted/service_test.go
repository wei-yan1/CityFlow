package trusted

import (
	"context"
	"errors"
	"testing"
	"time"

	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
)

type memoryStore struct {
	records map[string]location.TrustedPlace
	err     error
	writes  int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: make(map[string]location.TrustedPlace)}
}

func (m *memoryStore) SaveTrustedPlaces(_ context.Context, places []location.TrustedPlace) error {
	if m.err != nil {
		return m.err
	}
	m.writes++
	for _, place := range places {
		m.records[place.Place.ID] = place
	}
	return nil
}

func (m *memoryStore) FindTrustedPlaces(_ context.Context, ids []string, now time.Time) (map[string]location.TrustedPlace, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := make(map[string]location.TrustedPlace)
	for _, id := range ids {
		if place, ok := m.records[id]; ok && place.ExpiresAt.After(now) {
			out[id] = place
		}
	}
	return out, nil
}

func (m *memoryStore) PurgeTrustedPlaces(_ context.Context, before time.Time, limit int) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	if limit <= 0 {
		limit = 1000
	}
	var deleted int64
	for id, place := range m.records {
		if !place.ExpiresAt.After(before) {
			delete(m.records, id)
			deleted++
			if deleted >= int64(limit) {
				break
			}
		}
	}
	return deleted, nil
}

func testPlace(id, name string) location.Place {
	return location.Place{ID: id, Name: name, Address: "桂林", Location: location.Point{Latitude: 25.2, Longitude: 110.3}}
}

func TestRecordAndGetReusesVerifiedRows(t *testing.T) {
	store := newMemoryStore()
	service := New(store, time.Minute)
	first := testPlace("one", "原名称")
	if _, err := service.RecordAndGet(context.Background(), []location.Place{first}); err != nil {
		t.Fatal(err)
	}
	stored := store.records[first.ID]
	changed := first
	changed.Name = "搜索新名称"
	result, err := service.RecordAndGet(context.Background(), []location.Place{changed})
	if err != nil || len(result) != 1 || result[0].Name != first.Name || store.writes != 1 || !store.records[first.ID].ExpiresAt.Equal(stored.ExpiresAt) {
		t.Fatalf("reuse: %+v writes=%d err=%v", result, store.writes, err)
	}
}

func TestResolveRequiresVerifiedUnexpiredPlaces(t *testing.T) {
	store := newMemoryStore()
	service := New(store, time.Minute)
	ctx := context.Background()
	place := testPlace("guilin-poi", "地点")
	if _, err := service.Resolve(ctx, []string{place.ID, "invented"}); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("unknown accepted: %v", err)
	}
	if err := service.Record(ctx, []location.Place{place, {ID: "", Name: "missing id"}}); err != nil {
		t.Fatal(err)
	}
	got, err := service.Resolve(ctx, []string{place.ID, place.ID})
	if err != nil || len(got) != 2 || got[0] != place {
		t.Fatalf("provider result: %+v %v", got, err)
	}
	old := store.records[place.ID]
	old.ExpiresAt = time.Now().Add(-time.Second)
	store.records[place.ID] = old
	if _, err = service.Resolve(ctx, []string{place.ID, place.ID}); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("expired accepted: %v", err)
	}
	store.err = errors.New("offline")
	if _, err = service.Resolve(ctx, []string{place.ID, place.ID}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("failure must be closed: %v", err)
	}
}

func TestPurgeExpiredDeletesOnlyStaleRows(t *testing.T) {
	store := newMemoryStore()
	service := New(store, time.Minute)
	now := time.Now().UTC()
	store.records["stale"] = location.TrustedPlace{Place: testPlace("stale", "旧地点"), ExpiresAt: now.Add(-time.Hour)}
	store.records["fresh"] = location.TrustedPlace{Place: testPlace("fresh", "新地点"), ExpiresAt: now.Add(time.Hour)}

	deleted, err := service.PurgeExpired(context.Background(), now, 1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || len(store.records) != 1 {
		t.Fatalf("deleted=%d remaining=%d", deleted, len(store.records))
	}
	if _, ok := store.records["fresh"]; !ok {
		t.Fatalf("fresh record was deleted: %+v", store.records)
	}
}
