// Package discoverymaps contains map behavior owned by Discovery. The shared
// platform maps package only owns provider calls, caching and geometry.
package discoverymaps

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"cityflow/app/discovery/application/trusted"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/maps"
)

var amapPlaceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// TrustedSearch writes only genuine provider results within the service area.
type TrustedSearch struct {
	*maps.GuardedAmap
	Places *trusted.Service
	Adcode string
}

func (s *TrustedSearch) VerifyPlace(ctx context.Context, id string) error {
	if !amapPlaceID.MatchString(id) {
		return domain.ErrNotFound
	}
	provider, ok := s.Provider.(interface {
		PlaceByID(context.Context, string) (*maps.SearchResult, error)
	})
	if !ok {
		return domain.ErrUnavailable
	}
	if err := s.Cache.Allow(ctx); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	result, err := provider.PlaceByID(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	result, err = s.record(ctx, result)
	if err != nil {
		return err
	}
	if len(result.Data) == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *TrustedSearch) Search(ctx context.Context, keywords, city string) (*maps.SearchResult, error) {
	result, err := s.GuardedAmap.Search(ctx, keywords, city)
	if err != nil {
		return nil, err
	}
	return s.record(ctx, result)
}

func (s *TrustedSearch) Nearby(ctx context.Context, p location.Point) (*maps.SearchResult, error) {
	result, err := s.GuardedAmap.Nearby(ctx, p)
	if err != nil {
		return nil, err
	}
	return s.record(ctx, result)
}

func (s *TrustedSearch) record(ctx context.Context, result *maps.SearchResult) (*maps.SearchResult, error) {
	if len(result.Data) == 0 {
		result.Data = nil
		return result, nil
	}
	// A place that is already in the trusted store has already passed the
	// district check and is still inside its verification lifetime. Returning it
	// directly keeps repeated searches (including MySQL cache hits) working when
	// the district provider is slow or unavailable.
	if trustedPlaces, ok := s.knownPlaces(ctx, result.Data); ok {
		result.Data = trustedPlaces
		return result, nil
	}
	district, err := s.GuardedAmap.District(ctx, s.Adcode)
	if err != nil || district == nil || len(district.Boundaries) == 0 {
		return nil, fmt.Errorf("%w: 无法校验桂林服务范围", domain.ErrUnavailable)
	}
	valid := make([]location.Place, 0, len(result.Data))
	for _, p := range result.Data {
		if p.ID != "" && p.Name != "" && p.Location.Valid() && location.DistrictContains(district.Boundaries, p.Location) {
			valid = append(valid, p)
		}
	}
	valid, err = s.Places.RecordAndGet(ctx, valid)
	if err != nil {
		return nil, err
	}
	result.Data = valid
	return result, nil
}

// knownPlaces returns stored, unexpired versions only when every result has
// already been verified. A partial match must go through district filtering so
// a new or out-of-area POI cannot ride along with an old trusted one.
func (s *TrustedSearch) knownPlaces(ctx context.Context, places []location.Place) ([]location.Place, bool) {
	if s.Places == nil || len(places) == 0 {
		return nil, false
	}
	ids := make([]string, 0, len(places))
	seen := make(map[string]bool, len(places))
	for _, p := range places {
		if !amapPlaceID.MatchString(p.ID) || p.Name == "" || !p.Location.Valid() || seen[p.ID] {
			return nil, false
		}
		seen[p.ID] = true
		ids = append(ids, p.ID)
	}
	stored, err := s.Places.Find(ctx, ids, time.Now().UTC())
	if err != nil || len(stored) != len(ids) {
		return nil, false
	}
	out := make([]location.Place, 0, len(ids))
	for _, id := range ids {
		place, ok := stored[id]
		if !ok || place.Place.ID != id {
			return nil, false
		}
		out = append(out, place.Place)
	}
	return out, true
}
