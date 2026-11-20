package maps

import (
	"cityflow/internal/domain/location"
	"cityflow/internal/infrastructure/cache"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type MapProvider interface {
	Search(context.Context, string, string) (*SearchResult, error)
	Riding(context.Context, location.Point, location.Point) (*RidingRoute, error)
	City(context.Context, location.Point) (*City, error)
	District(context.Context, string) (*District, error)
}
type GuardedAmap struct {
	Provider    MapProvider
	Cache       *cache.MapCache
	PermitCache bool
	DistrictTTL time.Duration
	SearchTTL   time.Duration
	CityTTL     time.Duration
	RouteTTL    time.Duration
	HotCities   map[string]bool
}

func (g *GuardedAmap) ReverseAddress(ctx context.Context, p location.Point) (string, error) {
	provider, ok := g.Provider.(interface {
		ReverseAddress(context.Context, location.Point) (string, error)
	})
	if !ok {
		return "", fmt.Errorf("地址解析不可用")
	}
	key := fmt.Sprintf("amap:reverse:v1:%.6f,%.6f", p.Longitude, p.Latitude)
	var address string
	err := g.load(ctx, key, cache.MapPolicy{}, func(c context.Context) (any, error) {
		return provider.ReverseAddress(c, p)
	}, &address)
	if err != nil {
		return "", err
	}
	return address, nil
}

func providerTTL(value, fallback, maximum time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func (g *GuardedAmap) load(ctx context.Context, key string, p cache.MapPolicy, loader func(context.Context) (any, error), result any) error {
	if !g.PermitCache {
		p.TTL = 0
		p.Stale = 0
	}
	body, err := g.Cache.Load(ctx, key, p, func(inner context.Context) ([]byte, error) {
		if err := g.Cache.Allow(inner); err != nil {
			return nil, err
		}
		value, err := loader(inner)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(body, result)
}
func (g *GuardedAmap) Search(ctx context.Context, keywords, city string) (*SearchResult, error) {
	keywords = strings.Join(strings.Fields(keywords), " ")
	city = strings.TrimSpace(city)
	key := "amap:search:v1:" + city + ":" + strings.ToLower(keywords)
	result := &SearchResult{}
	err := g.load(ctx, key, cache.MapPolicy{TTL: providerTTL(g.SearchTTL, time.Minute, time.Minute), Force: g.HotCities[city]}, func(c context.Context) (any, error) {
		return g.Provider.Search(c, keywords, city)
	}, result)
	return result, err
}

// Nearby reuses only results for the exact same request coordinates. Grid
// snapping would change the provider's search center and could hide places.
func (g *GuardedAmap) Nearby(ctx context.Context, p location.Point) (*SearchResult, error) {
	provider, ok := g.Provider.(interface {
		Nearby(context.Context, location.Point) (*SearchResult, error)
	})
	if !ok {
		return nil, fmt.Errorf("附近地点查询不可用")
	}
	result := &SearchResult{}
	key := fmt.Sprintf("amap:nearby:v1:%.6f,%.6f", p.Longitude, p.Latitude)
	err := g.load(ctx, key, cache.MapPolicy{TTL: providerTTL(g.SearchTTL, 5*time.Minute, time.Hour), Force: true}, func(c context.Context) (any, error) {
		return provider.Nearby(c, p)
	}, result)
	return result, err
}
func (g *GuardedAmap) Riding(ctx context.Context, a, b location.Point) (*RidingRoute, error) {
	return g.RidingMode(ctx, "bicycling", a, b)
}

// RidingMode keeps the provider cache key mode-specific so bicycle, e-bike and
// driving plans cannot leak into one another.
func (g *GuardedAmap) RidingMode(ctx context.Context, mode string, a, b location.Point) (*RidingRoute, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "bicycling"
	}
	provider, ok := g.Provider.(interface {
		RidingMode(context.Context, string, location.Point, location.Point) (*RidingRoute, error)
	})
	if !ok {
		return nil, fmt.Errorf("路线方式暂不可用")
	}
	result := &RidingRoute{}
	key := fmt.Sprintf("amap:route:%s:v1:%.6f,%.6f:%.6f,%.6f", mode, a.Longitude, a.Latitude, b.Longitude, b.Latitude)
	err := g.load(ctx, key, cache.MapPolicy{TTL: providerTTL(g.RouteTTL, time.Minute, 5*time.Minute)}, func(c context.Context) (any, error) {
		return provider.RidingMode(c, mode, a, b)
	}, result)
	return result, err
}
func (g *GuardedAmap) City(ctx context.Context, p location.Point) (*City, error) {
	// Anonymous GCJ02 cell center only; never place user IDs or exact browser
	// coordinates in the shared cache or send its precise point to reverse lookup.
	const step = 0.01
	x, y := int(math.Floor(p.Longitude/step)), int(math.Floor(p.Latitude/step))
	center := location.Point{Longitude: (float64(x) + 0.5) * step, Latitude: (float64(y) + 0.5) * step}
	result := &City{}
	err := g.load(ctx, fmt.Sprintf("amap:city:grid:%d:%d", x, y), cache.MapPolicy{TTL: providerTTL(g.CityTTL, 10*time.Minute, time.Hour)}, func(c context.Context) (any, error) { return g.Provider.City(c, center) }, result)
	return result, err
}
func (g *GuardedAmap) District(ctx context.Context, code string) (*District, error) {
	result := &District{}
	ttl := g.DistrictTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	if ttl > 7*24*time.Hour {
		ttl = 7 * 24 * time.Hour
	}
	err := g.load(ctx, "amap:district:v1:"+code, cache.MapPolicy{TTL: ttl, Stale: ttl / 4, Force: g.HotCities[code]}, func(c context.Context) (any, error) { return g.Provider.District(c, code) }, result)
	return result, err
}
