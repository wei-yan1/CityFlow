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

type searchProviderSpy struct {
	MapProvider
	search atomic.Int32
}

func (p *searchProviderSpy) Search(context.Context, string, string) (*SearchResult, error) {
	p.search.Add(1)
	return &SearchResult{
		Provider:         "amap",
		CoordinateSystem: "GCJ02",
		Data:             []location.Place{{ID: "poi-1", Name: "Redis 缓存地点", Category: "POI", Address: "桂林市中心", Location: location.Point{Latitude: 25.274, Longitude: 110.290}}},
	}, nil
}

// TestSearchCacheSharedAcrossCallers proves the search path is read-through
// Redis: the first caller's provider call populates the cache and a second,
// independent caller with the same city/keywords is served without invoking
// its provider. The cache key intentionally contains no account identity.
func TestSearchCacheSharedAcrossCallers(t *testing.T) {
	address := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR for real Redis search checks")
	}
	ctx := context.Background()
	redis := cache.NewRedis(address, "", 0)
	if err := redis.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	defer redis.Close()
	mapCache := cache.NewMapCache(redis, 128<<20, 60)
	defer mapCache.Close()

	firstProvider := &searchProviderSpy{}
	first := &GuardedAmap{
		Provider:    firstProvider,
		Cache:       mapCache,
		PermitCache: true,
		SearchTTL:   time.Minute,
		HotCities:   map[string]bool{"桂林市": true},
	}
	keywords := "redis-cache-" + time.Now().Format("150405.000000")
	key := "amap:search:v1:桂林市:" + keywords
	defer func() { _ = mapCache.Invalidate(context.Background(), key) }()

	result, err := first.Search(ctx, keywords, "桂林市")
	if err != nil || len(result.Data) != 1 || result.Data[0].ID != "poi-1" {
		t.Fatalf("first search: result=%+v err=%v", result, err)
	}
	if got := firstProvider.search.Load(); got != 1 {
		t.Fatalf("first provider search calls = %d, want 1", got)
	}

	secondProvider := &searchProviderSpy{}
	second := &GuardedAmap{
		Provider:    secondProvider,
		Cache:       mapCache,
		PermitCache: true,
		SearchTTL:   time.Minute,
		HotCities:   map[string]bool{"桂林市": true},
	}
	result, err = second.Search(ctx, keywords, "桂林市")
	if err != nil || len(result.Data) != 1 || result.Data[0].ID != "poi-1" {
		t.Fatalf("second search: result=%+v err=%v", result, err)
	}
	if got := secondProvider.search.Load(); got != 0 {
		t.Fatalf("second provider search calls = %d, want 0 (Redis hit)", got)
	}
}
