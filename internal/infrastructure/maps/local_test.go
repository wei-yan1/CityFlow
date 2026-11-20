package maps

import (
	"context"
	"strings"
	"testing"
	"time"

	"cityflow/internal/application/service"
	"cityflow/internal/domain/location"
	"cityflow/internal/infrastructure/memory"
)

type stubSource struct {
	places       []location.Place
	boundary     *Boundary
	ways         []RoadWay
	nearbyCenter location.Point
	cityName     string
	cityAdcode   string
	cityCenter   *location.Point
	reverse      string
}

func (s *stubSource) SearchPlaces(context.Context, string, location.Point, int) ([]location.Place, error) {
	return s.places, nil
}

func (s *stubSource) NearbyPlaces(_ context.Context, center location.Point, _ float64, _ int) ([]location.Place, error) {
	s.nearbyCenter = center
	return s.places, nil
}

func (s *stubSource) Place(context.Context, string, int64) (location.Place, bool, error) {
	if len(s.places) == 0 {
		return location.Place{}, false, nil
	}
	return s.places[0], true, nil
}

func (s *stubSource) Boundary(context.Context, string) (*Boundary, error) { return s.boundary, nil }

func (s *stubSource) City(context.Context, location.Point) (string, string, *location.Point, error) {
	return s.cityName, s.cityAdcode, s.cityCenter, nil
}

func (s *stubSource) ReverseAddress(context.Context, location.Point, float64) (string, error) {
	return s.reverse, nil
}

func (s *stubSource) RoadWays(context.Context) ([]RoadWay, error) { return s.ways, nil }

func TestLocalSearchReturnsGCJ02AndOSMIDs(t *testing.T) {
	wgs := location.Point{Latitude: 25.274, Longitude: 110.290}
	source := &stubSource{places: []location.Place{{ID: "osm:n123", Name: "测试地点", Category: "school", Location: wgs}}}
	local := NewLocal(source, trustedPlacesForTest(), "桂林市", "450300")
	result, err := local.Search(context.Background(), "测试", "桂林市")
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "local" || result.CoordinateSystem != "GCJ02" || len(result.Data) != 1 {
		t.Fatalf("result: %+v", result)
	}
	want := wgs84ToGCJ(wgs)
	if result.Data[0].Location != want {
		t.Fatalf("coordinate not converted: got %+v want %+v", result.Data[0].Location, want)
	}
	if result.Data[0].ID != "osm:n123" || strings.TrimSpace(result.Data[0].Name) == "" {
		t.Fatalf("place: %+v", result.Data[0])
	}
}

func TestLocalNearbyAcceptsGCJ02Input(t *testing.T) {
	input := wgs84ToGCJ(location.Point{Latitude: 25.274, Longitude: 110.290})
	source := &stubSource{places: []location.Place{{ID: "osm:n1", Name: "附近", Location: location.Point{Latitude: 25.274, Longitude: 110.290}}}}
	local := NewLocal(source, trustedPlacesForTest(), "桂林市", "450300")
	if _, err := local.Nearby(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	want := GCJ02ToWGS84(input)
	if distance := haversine(source.nearbyCenter, want); distance > 1 {
		t.Fatalf("nearby center not converted back: got %+v want %+v (%.1f m)", source.nearbyCenter, want, distance)
	}
}

func TestLocalDistrictConvertsBoundaryAndServiceArea(t *testing.T) {
	boundary := &Boundary{
		Name:   "桂林市",
		Adcode: "450300",
		Center: location.Point{Latitude: 25.27, Longitude: 110.29},
		Rings: [][]location.Point{{
			{Latitude: 25.20, Longitude: 110.20},
			{Latitude: 25.20, Longitude: 110.40},
			{Latitude: 25.35, Longitude: 110.40},
			{Latitude: 25.35, Longitude: 110.20},
		}},
	}
	local := NewLocal(&stubSource{boundary: boundary}, trustedPlacesForTest(), "桂林市", "450300")
	district, err := local.District(context.Background(), "450300")
	if err != nil {
		t.Fatal(err)
	}
	if district.Name != "桂林市" || len(district.Boundaries) == 0 {
		t.Fatalf("district: %+v", district)
	}
	if district.Boundaries[0][0] != wgs84ToGCJ(boundary.Rings[0][0]) {
		t.Fatal("district ring not converted to GCJ02")
	}
	area := ServiceArea{Districts: local, Adcode: "450300"}
	if err := area.Validate(context.Background(), []location.Point{{Latitude: 25.27, Longitude: 110.29}}); err != nil {
		t.Fatalf("inside point rejected: %v", err)
	}
	if err := area.Validate(context.Background(), []location.Point{{Latitude: 26.50, Longitude: 111.50}}); err == nil {
		t.Fatal("outside point accepted")
	}
}

func TestLocalRouteUsesRoadGraph(t *testing.T) {
	ways := []RoadWay{
		{ID: 1, Name: "甲路", Highway: "residential", Points: []location.Point{
			{Latitude: 25.2700, Longitude: 110.2900}, {Latitude: 25.2700, Longitude: 110.3000},
		}},
		{ID: 2, Name: "乙路", Highway: "residential", Points: []location.Point{
			{Latitude: 25.2700, Longitude: 110.3000}, {Latitude: 25.2790, Longitude: 110.3000},
		}},
	}
	local := NewLocal(&stubSource{ways: ways}, trustedPlacesForTest(), "桂林市", "450300")
	origin := wgs84ToGCJ(location.Point{Latitude: 25.2701, Longitude: 110.2901})
	destination := wgs84ToGCJ(location.Point{Latitude: 25.2789, Longitude: 110.2999})
	route, err := local.Riding(context.Background(), origin, destination)
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "local" || route.CoordinateSystem != "GCJ02" {
		t.Fatalf("route meta: %+v", route)
	}
	if route.DistanceMeters < 1900 || route.DistanceMeters > 2200 {
		t.Fatalf("distance: %d", route.DistanceMeters)
	}
	if len(route.Coordinates) < 4 {
		t.Fatalf("route has no road shape: %+v", route.Coordinates)
	}
	joined := strings.Join(route.Instructions, "|")
	if !strings.Contains(joined, "甲路") || !strings.Contains(joined, "乙路") {
		t.Fatalf("instructions: %s", joined)
	}
	if len(route.Alternatives) != 1 || route.Alternatives[0].DistanceMeters != route.DistanceMeters {
		t.Fatalf("alternatives: %+v", route.Alternatives)
	}
}

func TestLocalRouteRespectsOneWay(t *testing.T) {
	ways := []RoadWay{
		{ID: 1, Name: "单行路", Highway: "residential", OneWay: 1, Points: []location.Point{
			{Latitude: 25.2700, Longitude: 110.2900}, {Latitude: 25.2700, Longitude: 110.3000},
		}},
	}
	local := NewLocal(&stubSource{ways: ways}, trustedPlacesForTest(), "桂林市", "450300")
	backward := wgs84ToGCJ(location.Point{Latitude: 25.2700, Longitude: 110.3000})
	forwardEnd := wgs84ToGCJ(location.Point{Latitude: 25.2700, Longitude: 110.2900})
	route, err := local.Riding(context.Background(), backward, forwardEnd)
	if err != nil {
		t.Fatal(err)
	}
	if route.Coordinates[0] != backward {
		t.Fatal("route does not start at origin")
	}
	if !strings.Contains(strings.Join(route.Instructions, "|"), "直线") {
		t.Fatalf("one-way edge should not be reversed: %v", route.Instructions)
	}
}

func TestLocalSearchRecordsTrustedPlaces(t *testing.T) {
	store := memory.NewTrustedPlaceStore()
	trusted := service.TrustedPlaces{Store: store, Lifetime: time.Hour}
	places := []location.Place{
		{ID: "osm:n1", Name: "甲地点", Category: "school", Location: location.Point{Latitude: 25.274, Longitude: 110.290}},
		{ID: "osm:n2", Name: "乙地点", Category: "hospital", Location: location.Point{Latitude: 25.275, Longitude: 110.291}},
	}
	local := NewLocal(&stubSource{places: places}, trusted, "桂林市", "450300")
	result, err := local.Search(context.Background(), "地点", "桂林市")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("trusted result: %+v", result.Data)
	}
	resolved, err := trusted.Resolve(context.Background(), []string{"osm:n1", "osm:n2"})
	if err != nil || len(resolved) != 2 {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	if resolved[0].ID != "osm:n1" || !resolved[0].Location.Valid() {
		t.Fatalf("resolved place: %+v", resolved[0])
	}
	if err := local.VerifyPlace(context.Background(), "osm:n1"); err != nil {
		t.Fatalf("verify local place: %v", err)
	}
	if err := local.VerifyPlace(context.Background(), "amap:1"); err == nil {
		t.Fatal("amap id accepted by the local provider")
	}
	// The stub ignores ids, so an unknown numeric id still resolves; only the
	// prefix format is rejected here.
	if err := local.VerifyPlace(context.Background(), "osm:x9"); err == nil {
		t.Fatal("malformed id accepted")
	}
}

func TestParseLocalPlaceID(t *testing.T) {
	for _, test := range []struct {
		id     string
		source string
		number int64
		ok     bool
	}{
		{"osm:n123", "node", 123, true},
		{"osm:w456", "way", 456, true},
		{"123", "", 0, false},
		{"osm:x1", "", 0, false},
		{"osm:w", "", 0, false},
	} {
		source, number, ok := parseLocalPlaceID(test.id)
		if ok != test.ok || source != test.source || number != test.number {
			t.Fatalf("%s: got %q %d %v", test.id, source, number, ok)
		}
	}
}

func TestLocalCityAndReverseAddress(t *testing.T) {
	center := location.Point{Latitude: 25.27, Longitude: 110.29}
	local := NewLocal(&stubSource{cityName: "桂林市", cityAdcode: "450300", cityCenter: &center, reverse: "中山中路附近"}, trustedPlacesForTest(), "桂林市", "450300")
	city, err := local.City(context.Background(), wgs84ToGCJ(center))
	if err != nil {
		t.Fatal(err)
	}
	if city.Adcode != "450300" || city.Name != "桂林市" || city.Center == nil {
		t.Fatalf("city: %+v", city)
	}
	if *city.Center != wgs84ToGCJ(center) {
		t.Fatal("city center not converted to GCJ02")
	}
	address, err := local.ReverseAddress(context.Background(), wgs84ToGCJ(center))
	if err != nil || address != "中山中路附近" {
		t.Fatalf("reverse: %q %v", address, err)
	}
}

// trustedPlacesForTest returns an empty store: recording is skipped and the
// provider returns the raw results, which is what these unit tests assert.
func trustedPlacesForTest() (places service.TrustedPlaces) { return }
