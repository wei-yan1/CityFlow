package maps

import (
	"cityflow/internal/application/service"
	"cityflow/internal/domain/location"
	"cityflow/internal/infrastructure/cache"
	"cityflow/internal/infrastructure/memory"
	"context"
	"testing"
	"time"
)

type detailProvider struct{ trustedProviderStub }

func (detailProvider) PlaceByID(_ context.Context, id string) (*SearchResult, error) {
	return &SearchResult{Data: []location.Place{{ID: id, Name: "地点", Location: location.Point{Latitude: 25, Longitude: 110}}}}, nil
}
func TestVerifyPlaceRecordsProviderDetail(t *testing.T) {
	c := cache.NewMapCache(nil, 0, 60)
	defer c.Close()
	store := memory.NewTrustedPlaceStore()
	s := TrustedSearch{GuardedAmap: &GuardedAmap{Provider: detailProvider{}, Cache: c}, Places: service.TrustedPlaces{Store: store, Lifetime: time.Minute}, Adcode: "450300"}
	if e := s.VerifyPlace(context.Background(), "B123"); e != nil {
		t.Fatal(e)
	}
	got, e := store.FindTrustedPlaces(context.Background(), []string{"B123"}, time.Now())
	if e != nil || len(got) != 1 {
		t.Fatalf("verified: %+v %v", got, e)
	}
	if e := s.VerifyPlace(context.Background(), "../outside"); e == nil {
		t.Fatal("invalid ID accepted")
	}
}
