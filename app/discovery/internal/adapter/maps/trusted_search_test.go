package discoverymaps

import (
	"context"
	"testing"
	"time"

	"cityflow/app/discovery/application/trusted"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/cache"
	platformmaps "cityflow/internal/platform/maps"
	"cityflow/internal/testfixture/memory"
)

type trustedProviderStub struct{ platformmaps.MapProvider }

func (trustedProviderStub) Search(context.Context, string, string) (*platformmaps.SearchResult, error) {
	return &platformmaps.SearchResult{Data: []location.Place{
		{ID: "inside", Name: "真实地点", Location: location.Point{Latitude: 25, Longitude: 110}},
		{ID: "outside", Name: "市外地点", Location: location.Point{Latitude: 30, Longitude: 120}},
	}}, nil
}

func (trustedProviderStub) District(context.Context, string) (*platformmaps.District, error) {
	return &platformmaps.District{Boundaries: [][]location.Point{{{Latitude: 24, Longitude: 109}, {Latitude: 24, Longitude: 111}, {Latitude: 26, Longitude: 111}, {Latitude: 26, Longitude: 109}}}}, nil
}

func TestTrustedSearchOnlyRecordsResultsInsideDistrict(t *testing.T) {
	c := cache.NewMapCache(nil, 0, 60)
	defer c.Close()
	store := memory.NewTrustedPlaceStore()
	service := &TrustedSearch{
		GuardedAmap: &platformmaps.GuardedAmap{Provider: trustedProviderStub{}, Cache: c},
		Places:      trusted.New(store, time.Minute),
		Adcode:      "450300",
	}
	result, err := service.Search(context.Background(), "地点", "桂林")
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

func (p *trustedOnlyProvider) Search(context.Context, string, string) (*platformmaps.SearchResult, error) {
	return &platformmaps.SearchResult{Data: []location.Place{{ID: "inside", Name: "真实地点", Location: location.Point{Latitude: 25, Longitude: 110}}}}, nil
}

func (p *trustedOnlyProvider) District(context.Context, string) (*platformmaps.District, error) {
	p.districtCalls++
	return &platformmaps.District{Boundaries: [][]location.Point{{{Latitude: 24, Longitude: 109}, {Latitude: 24, Longitude: 111}, {Latitude: 26, Longitude: 111}, {Latitude: 26, Longitude: 109}}}}, nil
}

func TestTrustedSearchSkipsDistrictForAlreadyVerifiedPlaces(t *testing.T) {
	c := cache.NewMapCache(nil, 0, 60)
	defer c.Close()
	store := memory.NewTrustedPlaceStore()
	provider := &trustedOnlyProvider{}
	service := &TrustedSearch{
		GuardedAmap: &platformmaps.GuardedAmap{Provider: provider, Cache: c},
		Places:      trusted.New(store, time.Minute),
		Adcode:      "450300",
	}
	if _, err := service.Search(context.Background(), "地点", "桂林"); err != nil {
		t.Fatal(err)
	}
	if provider.districtCalls != 1 {
		t.Fatalf("first district calls = %d, want 1", provider.districtCalls)
	}
	if _, err := service.Search(context.Background(), "地点", "桂林"); err != nil {
		t.Fatal(err)
	}
	if provider.districtCalls != 1 {
		t.Fatalf("district calls after trusted hit = %d, want 1", provider.districtCalls)
	}
}

type detailProvider struct{ trustedProviderStub }

func (detailProvider) PlaceByID(_ context.Context, id string) (*platformmaps.SearchResult, error) {
	return &platformmaps.SearchResult{Data: []location.Place{{ID: id, Name: "地点", Location: location.Point{Latitude: 25, Longitude: 110}}}}, nil
}

func TestVerifyPlaceRecordsProviderDetail(t *testing.T) {
	c := cache.NewMapCache(nil, 0, 60)
	defer c.Close()
	store := memory.NewTrustedPlaceStore()
	service := &TrustedSearch{
		GuardedAmap: &platformmaps.GuardedAmap{Provider: detailProvider{}, Cache: c},
		Places:      trusted.New(store, time.Minute),
		Adcode:      "450300",
	}
	if err := service.VerifyPlace(context.Background(), "B123"); err != nil {
		t.Fatal(err)
	}
	got, err := store.FindTrustedPlaces(context.Background(), []string{"B123"}, time.Now())
	if err != nil || len(got) != 1 {
		t.Fatalf("verified: %+v %v", got, err)
	}
	if err := service.VerifyPlace(context.Background(), "../outside"); err == nil {
		t.Fatal("invalid ID accepted")
	}
}
