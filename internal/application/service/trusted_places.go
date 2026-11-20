package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
)

// TrustedPlaces stores only provider-originated POIs. The caller of Record must
// be the provider integration, never an HTTP request accepting place objects.
type TrustedPlaces struct {
	Store    repository.TrustedPlaces
	Lifetime time.Duration
}

func (s TrustedPlaces) Record(ctx context.Context, places []location.Place) error {
	_, err := s.RecordAndGet(ctx, places)
	return err
}

// RecordAndGet returns the stored versions, so callers display exactly the
// information that task creation will later read from the trusted store.
func (s TrustedPlaces) RecordAndGet(ctx context.Context, places []location.Place) ([]location.Place, error) {
	if s.Store == nil || s.Lifetime <= 0 {
		return nil, fmt.Errorf("%w: 地点验证服务不可用", domain.ErrUnavailable)
	}
	now := time.Now().UTC()
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
	records := make([]repository.TrustedPlace, 0, len(valid))
	for _, p := range valid {
		if _, ok := existing[p.ID]; !ok {
			records = append(records, repository.TrustedPlace{Place: p, VerifiedAt: now, ExpiresAt: now.Add(s.Lifetime)})
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
func (s TrustedPlaces) Resolve(ctx context.Context, ids []string) ([]location.Place, error) {
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
	results, err := s.Store.FindTrustedPlaces(ctx, ids, time.Now().UTC())
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
