package maps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
)

// OnlineMapService is the online provider surface Hybrid prefers. It matches
// *TrustedSearch and lets tests inject a lightweight stub.
type OnlineMapService interface {
	Search(context.Context, string, string) (*SearchResult, error)
	Nearby(context.Context, location.Point) (*SearchResult, error)
	Riding(context.Context, location.Point, location.Point) (*RidingRoute, error)
	RidingMode(context.Context, string, location.Point, location.Point) (*RidingRoute, error)
	City(context.Context, location.Point) (*City, error)
	District(context.Context, string) (*District, error)
	ReverseAddress(context.Context, location.Point) (string, error)
	VerifyPlace(context.Context, string) error
}

// Hybrid composes an online provider with the local PostGIS provider. The
// online path wins when it returns a usable value; otherwise the local path
// serves the request. Each method only supplies two closures to serve, so the
// fallback policy lives in one place.
type Hybrid struct {
	Online OnlineMapService
	Local  *Local

	breaker onlineBreaker
}

func NewHybrid(online OnlineMapService, local *Local) *Hybrid {
	return &Hybrid{Online: online, Local: local}
}

// onlineBreaker stops hammering a failing provider. Amap has a 10s client
// timeout; without a cooldown every request would wait the full timeout before
// reaching the local fallback.
type onlineBreaker struct {
	mu       sync.Mutex
	until    time.Time
	cooldown time.Duration
	clock    func() time.Time
}

const defaultOnlineCooldown = 30 * time.Second

func (b *onlineBreaker) now() time.Time {
	if b.clock != nil {
		return b.clock()
	}
	return time.Now()
}

func (b *onlineBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.now().Before(b.until)
}

func (b *onlineBreaker) fail(err error) {
	if err == nil || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrInvalidRequest) {
		return
	}
	cooldown := b.cooldown
	if cooldown <= 0 {
		cooldown = defaultOnlineCooldown
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.until = b.now().Add(cooldown)
}

// serve applies one fallback policy to every Hybrid endpoint: try online while
// the breaker is closed, accept the value only when usable, then use local.
func serve[T any](
	h *Hybrid,
	online func(OnlineMapService) (T, error),
	local func(*Local) (T, error),
	usable func(T) bool,
	unavailable string,
) (T, error) {
	var zero T
	if h.Online != nil && h.breaker.allow() {
		value, err := online(h.Online)
		if err == nil && usable(value) {
			return value, nil
		}
		h.breaker.fail(err)
	}
	if h.Local != nil {
		return local(h.Local)
	}
	return zero, fmt.Errorf("%w: %s", domain.ErrUnavailable, unavailable)
}

func (h *Hybrid) Search(ctx context.Context, keywords, city string) (*SearchResult, error) {
	return serve(h,
		func(o OnlineMapService) (*SearchResult, error) { return o.Search(ctx, keywords, city) },
		func(l *Local) (*SearchResult, error) { return l.Search(ctx, keywords, city) },
		func(r *SearchResult) bool { return r != nil && len(r.Data) > 0 },
		"地图搜索暂不可用")
}

func (h *Hybrid) Nearby(ctx context.Context, p location.Point) (*SearchResult, error) {
	return serve(h,
		func(o OnlineMapService) (*SearchResult, error) { return o.Nearby(ctx, p) },
		func(l *Local) (*SearchResult, error) { return l.Nearby(ctx, p) },
		func(r *SearchResult) bool { return r != nil && len(r.Data) > 0 },
		"附近地点查询暂不可用")
}

func (h *Hybrid) Riding(ctx context.Context, origin, destination location.Point) (*RidingRoute, error) {
	return h.RidingMode(ctx, "bicycling", origin, destination)
}

func (h *Hybrid) RidingMode(ctx context.Context, mode string, origin, destination location.Point) (*RidingRoute, error) {
	return serve(h,
		func(o OnlineMapService) (*RidingRoute, error) { return o.RidingMode(ctx, mode, origin, destination) },
		func(l *Local) (*RidingRoute, error) { return l.RidingMode(ctx, mode, origin, destination) },
		func(r *RidingRoute) bool { return r != nil && len(r.Coordinates) >= 2 && r.DistanceMeters > 0 },
		"路线规划暂不可用")
}

func (h *Hybrid) City(ctx context.Context, p location.Point) (*City, error) {
	return serve(h,
		func(o OnlineMapService) (*City, error) { return o.City(ctx, p) },
		func(l *Local) (*City, error) { return l.City(ctx, p) },
		func(c *City) bool { return c != nil && strings.TrimSpace(c.Name) != "" },
		"城市识别暂不可用")
}

func (h *Hybrid) District(ctx context.Context, adcode string) (*District, error) {
	return serve(h,
		func(o OnlineMapService) (*District, error) { return o.District(ctx, adcode) },
		func(l *Local) (*District, error) { return l.District(ctx, adcode) },
		func(d *District) bool { return d != nil && len(d.Boundaries) > 0 },
		"行政区边界暂不可用")
}

func (h *Hybrid) ReverseAddress(ctx context.Context, p location.Point) (string, error) {
	return serve(h,
		func(o OnlineMapService) (string, error) { return o.ReverseAddress(ctx, p) },
		func(l *Local) (string, error) { return l.ReverseAddress(ctx, p) },
		func(address string) bool { return strings.TrimSpace(address) != "" },
		"地址解析暂不可用")
}

// VerifyPlace dispatches by id namespace: Amap ids stay online, osm ids stay
// local. This keeps the trusted-place flow working for both result sources.
func (h *Hybrid) VerifyPlace(ctx context.Context, id string) error {
	if strings.HasPrefix(id, "osm:") {
		if h.Local == nil {
			return domain.ErrUnavailable
		}
		return h.Local.VerifyPlace(ctx, id)
	}
	if h.Online == nil {
		return domain.ErrUnavailable
	}
	return h.Online.VerifyPlace(ctx, id)
}
