// Package trusted owns the provider-verified place lifecycle for Discovery.
// Other services must resolve places through Discovery instead of importing
// this package or reading trusted_places directly.
package trusted

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cityflow/app/discovery/repository"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
)

type Service struct {
	Store    repository.TrustedPlaces
	Lifetime time.Duration
	now      func() time.Time
}

func New(store repository.TrustedPlaces, lifetime time.Duration) *Service {
	return &Service{Store: store, Lifetime: lifetime, now: time.Now}
}

func (s *Service) nowUTC() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

// Find returns raw unexpired records. It is a repository-facing read for the
// provider integration; callers outside Discovery should use Resolve through
// the RPC boundary.
func (s *Service) Find(ctx context.Context, ids []string, now time.Time) (map[string]location.TrustedPlace, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("%w: 地点验证服务不可用", domain.ErrUnavailable)
	}
	return s.Store.FindTrustedPlaces(ctx, ids, now.UTC())
}

func (s *Service) Record(ctx context.Context, places []location.Place) error {
	_, err := s.RecordAndGet(ctx, places)
	return err
}

// PurgeExpired removes globally stale verification rows. The targeted delete
// in SaveTrustedPlaces stays because it is what lets an expired POI be
// re-verified; this loop handles POIs that are never searched again.
func (s *Service) PurgeExpired(ctx context.Context, before time.Time, batch, maxPasses int) (int64, error) {
	if s.Store == nil {
		return 0, fmt.Errorf("%w: 地点验证服务不可用", domain.ErrUnavailable)
	}
	purger, ok := s.Store.(repository.TrustedPlacePurger)
	if !ok {
		return 0, fmt.Errorf("%w: 地点清理任务不可用", domain.ErrUnavailable)
	}
	if batch <= 0 {
		batch = 1000
	}
	if maxPasses <= 0 {
		maxPasses = 5
	}
	var total int64
	for pass := 0; pass < maxPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := purger.PurgeTrustedPlaces(ctx, before, batch)
		if err != nil {
			return total, fmt.Errorf("%w: 地点清理失败", domain.ErrUnavailable)
		}
		total += deleted
		if deleted < int64(batch) {
			break
		}
	}
	return total, nil
}

// RecordAndGet returns the stored versions, so callers display exactly the
// information task creation will later read from the trusted store.
func (s *Service) RecordAndGet(ctx context.Context, places []location.Place) ([]location.Place, error) {
	if s.Store == nil || s.Lifetime <= 0 {
		return nil, fmt.Errorf("%w: 地点验证服务不可用", domain.ErrUnavailable)
	}
	now := s.nowUTC()
	valid := make([]location.Place, 0, len(places))
	ids := make([]string, 0, len(places))
	seen := make(map[string]bool)
	for _, p := range places {
		if p.ID == "" || len(p.ID) > 128 || strings.TrimSpace(p.ID) != p.ID || strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 255 || len([]rune(p.Address)) > 1000 || !p.Location.Valid() || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		valid = append(valid, p)
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		return valid, nil
	}
	existing, err := s.Store.FindTrustedPlaces(ctx, ids, now)
	if err != nil {
		return nil, fmt.Errorf("%w: 地点信息暂时无法读取", domain.ErrUnavailable)
	}
	records := make([]location.TrustedPlace, 0, len(valid))
	for _, p := range valid {
		if _, ok := existing[p.ID]; !ok {
			records = append(records, location.TrustedPlace{Place: p, VerifiedAt: now, ExpiresAt: now.Add(s.Lifetime)})
		}
	}
	if len(records) != 0 {
		if err := s.Store.SaveTrustedPlaces(ctx, records); err != nil {
			return nil, fmt.Errorf("%w: 地点信息暂时无法保存", domain.ErrUnavailable)
		}
		// Another request may have inserted the same ID after our first read.
		existing, err = s.Store.FindTrustedPlaces(ctx, ids, now)
		if err != nil {
			return nil, fmt.Errorf("%w: 地点信息暂时无法读取", domain.ErrUnavailable)
		}
	}
	result := make([]location.Place, 0, len(ids))
	for _, id := range ids {
		if p, ok := existing[id]; ok {
			result = append(result, p.Place)
		}
	}
	return result, nil
}

// Resolve does a bounded local lookup only, and never calls the map provider.
// It preserves caller order and reports the first missing or expired location.
func (s *Service) Resolve(ctx context.Context, ids []string) ([]location.Place, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("%w: 地点验证服务不可用", domain.ErrUnavailable)
	}
	if len(ids) < 2 || len(ids) > 51 {
		return nil, fmt.Errorf("%w: 取送地点数量无效", domain.ErrInvalidRequest)
	}
	for i, id := range ids {
		if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
			return nil, fmt.Errorf("%w: 第 %d 个地点无效，请重新搜索选择", domain.ErrInvalidRequest, i+1)
		}
	}
	results, err := s.Store.FindTrustedPlaces(ctx, ids, s.nowUTC())
	if err != nil {
		return nil, fmt.Errorf("%w: 地点验证服务不可用", domain.ErrUnavailable)
	}
	resolved := make([]location.Place, len(ids))
	for i, id := range ids {
		place, ok := results[id]
		if !ok || place.Place.ID != id || !place.Place.Location.Valid() || strings.TrimSpace(place.Place.Name) == "" {
			return nil, fmt.Errorf("%w: 第 %d 个地点未验证或已过期，请重新搜索选择", domain.ErrInvalidRequest, i+1)
		}
		resolved[i] = place.Place
	}
	return resolved, nil
}
