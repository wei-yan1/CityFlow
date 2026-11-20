package maps

import (
	"context"
	"errors"
	"testing"
	"time"

	"cityflow/internal/application/service"
	"cityflow/internal/domain/location"
	"cityflow/internal/infrastructure/memory"
)

type stubOnline struct {
	search      *SearchResult
	searchErr   error
	searchCalls int
	nearby      *SearchResult
	nearbyErr   error
	route       *RidingRoute
	routeErr    error
	city        *City
	cityErr     error
	district    *District
	districtErr error
	reverse     string
	reverseErr  error
	verified    []string
	verifyErr   error
}

func (s *stubOnline) Search(context.Context, string, string) (*SearchResult, error) {
	s.searchCalls++
	return s.search, s.searchErr
}

func (s *stubOnline) Nearby(context.Context, location.Point) (*SearchResult, error) {
	return s.nearby, s.nearbyErr
}

func (s *stubOnline) Riding(context.Context, location.Point, location.Point) (*RidingRoute, error) {
	return s.route, s.routeErr
}

func (s *stubOnline) RidingMode(context.Context, string, location.Point, location.Point) (*RidingRoute, error) {
	return s.route, s.routeErr
}

func (s *stubOnline) City(context.Context, location.Point) (*City, error) { return s.city, s.cityErr }

func (s *stubOnline) District(context.Context, string) (*District, error) {
	return s.district, s.districtErr
}

func (s *stubOnline) ReverseAddress(context.Context, location.Point) (string, error) {
	return s.reverse, s.reverseErr
}

func (s *stubOnline) VerifyPlace(_ context.Context, id string) error {
	s.verified = append(s.verified, id)
	return s.verifyErr
}

func TestHybridPrefersOnlineThenFallsBack(t *testing.T) {
	local := NewLocal(&stubSource{places: []location.Place{{ID: "osm:n1", Name: "本地地点", Location: location.Point{Latitude: 25.27, Longitude: 110.29}}}}, service.TrustedPlaces{}, "桂林市", "450300")
	online := &stubOnline{search: &SearchResult{Provider: "amap", CoordinateSystem: "GCJ02", Data: []location.Place{{ID: "B1", Name: "在线地点", Location: location.Point{Latitude: 25.27, Longitude: 110.29}}}}}
	hybrid := &Hybrid{Online: online, Local: local}
	result, err := hybrid.Search(context.Background(), "地点", "桂林市")
	if err != nil || result.Provider != "amap" || result.Data[0].ID != "B1" {
		t.Fatalf("online preference: %+v %v", result, err)
	}

	online.searchErr = errors.New("quota exhausted")
	result, err = hybrid.Search(context.Background(), "地点", "桂林市")
	if err != nil || result.Provider != "local" || result.Data[0].ID != "osm:n1" {
		t.Fatalf("search fallback: %+v %v", result, err)
	}

	online.searchErr = nil
	online.search = &SearchResult{Provider: "amap", Data: []location.Place{}}
	result, err = hybrid.Search(context.Background(), "地点", "桂林市")
	if err != nil || result.Provider != "local" {
		t.Fatalf("empty online result must fall back: %+v %v", result, err)
	}
}

func TestHybridFallsBackForDistrictRouteAndReverse(t *testing.T) {
	boundary := &Boundary{
		Name: "桂林市", Adcode: "450300", Center: location.Point{Latitude: 25.27, Longitude: 110.29},
		Rings: [][]location.Point{{
			{Latitude: 25.20, Longitude: 110.20}, {Latitude: 25.20, Longitude: 110.40},
			{Latitude: 25.35, Longitude: 110.40}, {Latitude: 25.35, Longitude: 110.20},
		}},
	}
	ways := []RoadWay{{ID: 1, Name: "甲路", Highway: "residential", Points: []location.Point{
		{Latitude: 25.2700, Longitude: 110.2900}, {Latitude: 25.2700, Longitude: 110.3000},
	}}}
	source := &stubSource{boundary: boundary, ways: ways, reverse: "本地地址"}
	local := NewLocal(source, service.TrustedPlaces{}, "桂林市", "450300")
	online := &stubOnline{districtErr: errors.New("amap down"), routeErr: errors.New("amap down"), reverseErr: errors.New("amap down")}
	hybrid := &Hybrid{Online: online, Local: local}

	district, err := hybrid.District(context.Background(), "450300")
	if err != nil || len(district.Boundaries) == 0 || district.Name != "桂林市" {
		t.Fatalf("district fallback: %+v %v", district, err)
	}
	area := ServiceArea{Districts: hybrid, Adcode: "450300"}
	if err := area.Validate(context.Background(), []location.Point{{Latitude: 25.27, Longitude: 110.29}}); err != nil {
		t.Fatalf("service area via hybrid: %v", err)
	}

	origin := wgs84ToGCJ(location.Point{Latitude: 25.2701, Longitude: 110.2901})
	destination := wgs84ToGCJ(location.Point{Latitude: 25.2709, Longitude: 110.2999})
	route, err := hybrid.RidingMode(context.Background(), "bicycling", origin, destination)
	if err != nil || route.Provider != "local" || route.DistanceMeters < 900 {
		t.Fatalf("route fallback: %+v %v", route, err)
	}

	address, err := hybrid.ReverseAddress(context.Background(), origin)
	if err != nil || address != "本地地址" {
		t.Fatalf("reverse fallback: %q %v", address, err)
	}
}

func TestHybridCooldownSkipsOnlineAfterFailure(t *testing.T) {
	online := &stubOnline{searchErr: errors.New("provider timeout")}
	local := NewLocal(&stubSource{places: []location.Place{{ID: "osm:n1", Name: "本地地点", Location: location.Point{Latitude: 25.27, Longitude: 110.29}}}}, service.TrustedPlaces{}, "桂林市", "450300")
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	hybrid := NewHybrid(online, local)
	hybrid.breaker.clock = func() time.Time { return now }

	if _, err := hybrid.Search(context.Background(), "地点", "桂林市"); err != nil {
		t.Fatal(err)
	}
	if online.searchCalls != 1 {
		t.Fatalf("first call: %d", online.searchCalls)
	}
	if _, err := hybrid.Search(context.Background(), "地点", "桂林市"); err != nil {
		t.Fatal(err)
	}
	if online.searchCalls != 1 {
		t.Fatalf("cooldown did not skip the online provider: %d", online.searchCalls)
	}
	now = now.Add(defaultOnlineCooldown + time.Second)
	if _, err := hybrid.Search(context.Background(), "地点", "桂林市"); err != nil {
		t.Fatal(err)
	}
	if online.searchCalls != 2 {
		t.Fatalf("cooldown expiry did not retry online: %d", online.searchCalls)
	}
}

func TestHybridVerifyPlaceDispatchesByID(t *testing.T) {
	store := memory.NewTrustedPlaceStore()
	trusted := service.TrustedPlaces{Store: store, Lifetime: time.Hour}
	local := NewLocal(&stubSource{places: []location.Place{{ID: "osm:n1", Name: "本地地点", Location: location.Point{Latitude: 25.27, Longitude: 110.29}}}}, trusted, "桂林市", "450300")
	online := &stubOnline{}
	hybrid := &Hybrid{Online: online, Local: local}

	if err := hybrid.VerifyPlace(context.Background(), "B123"); err != nil {
		t.Fatalf("amap verify: %v", err)
	}
	if len(online.verified) != 1 || online.verified[0] != "B123" {
		t.Fatalf("online verify calls: %v", online.verified)
	}
	if err := hybrid.VerifyPlace(context.Background(), "osm:n1"); err != nil {
		t.Fatalf("local verify: %v", err)
	}
	if len(online.verified) != 1 {
		t.Fatalf("osm id must not reach amap: %v", online.verified)
	}
	resolved, err := trusted.Resolve(context.Background(), []string{"osm:n1", "osm:n1"})
	if err != nil || len(resolved) != 2 || resolved[0].ID != "osm:n1" {
		t.Fatalf("trusted store: %+v %v", resolved, err)
	}
}
