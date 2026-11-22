package postgis

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cityflow/internal/common/location"
	"cityflow/internal/platform/maps"
)

// This test needs the imported Guilin layers:
//
//	CITYFLOW_MAP_TEST_DSN=postgres://cityflow:<password>@127.0.0.1:5434/cityflow_map?sslmode=disable
//
// It is skipped when the variable is unset so unit test runs stay hermetic.
func TestStoreAgainstGuilinDatabase(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CITYFLOW_MAP_TEST_DSN"))
	if dsn == "" {
		t.Skip("CITYFLOW_MAP_TEST_DSN not set")
	}
	ctx := context.Background()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	center := location.Point{Latitude: 25.274, Longitude: 110.290}
	places, err := store.SearchPlaces(ctx, "医院", center, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(places) == 0 {
		t.Fatal("keyword search returned no places")
	}
	if !strings.HasPrefix(places[0].ID, "osm:") {
		t.Fatalf("unexpected id %q", places[0].ID)
	}

	nearby, err := store.NearbyPlaces(ctx, center, 2000, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(nearby) < 5 {
		t.Fatalf("nearby returned %d places", len(nearby))
	}

	boundary, err := store.Boundary(ctx, "450300")
	if err != nil || boundary == nil || len(boundary.Rings) == 0 {
		t.Fatalf("boundary: %+v %v", boundary, err)
	}
	district, err := store.Boundary(ctx, "450321")
	if err != nil || district == nil {
		t.Logf("district 450321 unavailable: %v", err)
	}

	name, adcode, _, err := store.City(ctx, center)
	if err != nil || name != "桂林市" || adcode != "450300" {
		t.Fatalf("city: %q %q %v", name, adcode, err)
	}

	address, err := store.ReverseAddress(ctx, center, 60)
	if err != nil || strings.TrimSpace(address) == "" {
		t.Fatalf("reverse address: %q %v", address, err)
	}

	start := time.Now()
	ways, err := store.RoadWays(ctx)
	if err != nil || len(ways) < 1000 {
		t.Fatalf("road ways: %d %v", len(ways), err)
	}
	t.Logf("loaded %d road ways in %s", len(ways), time.Since(start).Round(time.Millisecond))

	local := maps.NewLocal(store, nil, "桂林市", "450300")
	local.Warm(ctx)
	route, err := local.Riding(ctx,
		maps.WGS84ToGCJ(location.Point{Latitude: 25.274, Longitude: 110.290}),
		maps.WGS84ToGCJ(location.Point{Latitude: 25.310, Longitude: 110.350}))
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "local" || route.DistanceMeters < 4000 || len(route.Coordinates) < 10 {
		t.Fatalf("route: provider=%s distance=%d points=%d", route.Provider, route.DistanceMeters, len(route.Coordinates))
	}
	t.Logf("route %d m / %d s / %d points / %d legs; first instructions: %v",
		route.DistanceMeters, route.DurationSeconds, len(route.Coordinates), len(route.Alternatives), route.Instructions)
	t.Logf("search hit: %s (%s); nearby hits: %d; reverse: %s", places[0].Name, places[0].Category, len(nearby), address)
}
