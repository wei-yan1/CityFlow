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

type trustedProviderStub struct{ MapProvider }

func (trustedProviderStub) Search(context.Context, string, string) (*SearchResult, error) {
	return &SearchResult{Data: []location.Place{
		{ID: "inside", Name: "真实地点", Location: location.Point{Latitude: 25, Longitude: 110}},
		{ID: "outside", Name: "市外地点", Location: location.Point{Latitude: 30, Longitude: 120}},
	}}, nil
}
func (trustedProviderStub) District(context.Context, string) (*District, error) {
	return &District{Boundaries: [][]location.Point{{{Latitude: 24, Longitude: 109}, {Latitude: 24, Longitude: 111}, {Latitude: 26, Longitude: 111}, {Latitude: 26, Longitude: 109}}}}, nil
}

func TestTrustedSearchOnlyRecordsResultsInsideDistrict(t *testing.T) {
	cache := cache.NewMapCache(nil, 0, 60)
	defer cache.Close()
	store := memory.NewTrustedPlaceStore()
	trusted := service.TrustedPlaces{Store: store, Lifetime: time.Minute}
	provider := &TrustedSearch{GuardedAmap: &GuardedAmap{Provider: trustedProviderStub{}, Cache: cache}, Places: trusted, Adcode: "450300"}
	result, err := provider.Search(context.Background(), "地点", "桂林")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 || result.Data[0].ID != "inside" {
		t.Fatalf("filter: %+v", result.Data)
	}
	values, err := store.FindTrustedPlaces(context.Background(), []string{"inside", "outside"}, time.Now())
	if err != nil || len(values) != 1 {
		t.Fatalf("records: %+v %v", values, err)
	}
}

type trustedOnlyProvider struct {
	trustedProviderStub
	districtCalls int
}

func (p *trustedOnlyProvider) Search(context.Context, string, string) (*SearchResult, error) {
	return &SearchResult{Data: []location.Place{{ID: "inside", Name: "真实地点", Location: location.Point{Latitude: 25, Longitude: 110}}}}, nil
}

func (p *trustedOnlyProvider) District(context.Context, string) (*District, error) {
	p.districtCalls++
	return &District{Boundaries: [][]location.Point{{{Latitude: 24, Longitude: 109}, {Latitude: 24, Longitude: 111}, {Latitude: 26, Longitude: 111}, {Latitude: 26, Longitude: 109}}}}, nil
}

func TestTrustedSearchSkipsDistrictForAlreadyVerifiedPlaces(t *testing.T) {
	cache := cache.NewMapCache(nil, 0, 60)
	defer cache.Close()
	store := memory.NewTrustedPlaceStore()
	trusted := service.TrustedPlaces{Store: store, Lifetime: time.Minute}
	provider := &trustedOnlyProvider{}
	search := &TrustedSearch{GuardedAmap: &GuardedAmap{Provider: provider, Cache: cache}, Places: trusted, Adcode: "450300"}

	if _, err := search.Search(context.Background(), "地点", "桂林"); err != nil {
		t.Fatal(err)
	}
	if provider.districtCalls != 1 {
		t.Fatalf("first district calls = %d, want 1", provider.districtCalls)
	}
	if _, err := search.Search(context.Background(), "地点", "桂林"); err != nil {
		t.Fatal(err)
	}
	if provider.districtCalls != 1 {
		t.Fatalf("district calls after trusted hit = %d, want 1", provider.districtCalls)
	}
}
