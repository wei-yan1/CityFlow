package maps

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"cityflow/internal/common/location"
	"cityflow/internal/platform/cache"
)

type nearbyCounter struct {
	MapProvider
	calls atomic.Int32
}

func (s *nearbyCounter) Nearby(_ context.Context, p location.Point) (*SearchResult, error) {
	s.calls.Add(1)
	return &SearchResult{Data: []location.Place{{ID: "test", Name: "test", Location: p}}}, nil
}
func TestNearbyUsesRedisForIdenticalCoordinates(t *testing.T) {
	address := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR to test Redis")
	}
	r := cache.NewRedis(address, "", 0)
	defer r.Close()
	if err := r.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := cache.NewMapCache(r, 128<<20, 60)
	defer c.Close()
	source := &nearbyCounter{}
	g := &GuardedAmap{Provider: source, Cache: c, PermitCache: true, SearchTTL: time.Second}
	point := location.Point{Latitude: 25.2 + float64(time.Now().UnixNano()%1000000)/1e12, Longitude: 110.3}
	for i := 0; i < 2; i++ {
		got, err := g.Nearby(context.Background(), point)
		if err != nil || len(got.Data) != 1 || got.Data[0].Location != point {
			t.Fatalf("nearby: %+v %v", got, err)
		}
	}
	if n := source.calls.Load(); n != 1 {
		t.Fatalf("provider called %d times, expected 1", n)
	}
	point.Latitude += 0.000001
	if _, err := g.Nearby(context.Background(), point); err != nil {
		t.Fatal(err)
	}
	if n := source.calls.Load(); n != 2 {
		t.Fatalf("distinct coordinates reused another result; calls=%d", n)
	}
}
