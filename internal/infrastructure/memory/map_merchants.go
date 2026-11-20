package memory

import (
	"cityflow/internal/domain/location"
	"context"
	"sort"
)

func (s *Store) MapMerchants(ctx context.Context, b location.Bounds, limit int) ([]location.Place, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]location.Place, 0)
	for _, p := range s.merchants {
		account := s.users[p.UserID]
		point := location.Point{Latitude: p.Latitude, Longitude: p.Longitude}
		if !p.Archived && p.BusinessStatus == "OPEN" && account != nil && account.Status == "ACTIVE" && b.Contains(point) && point.Longitude < b.East && point.Latitude < b.North {
			result = append(result, location.Place{ID: p.ID, Name: p.Name, Address: p.Address, Category: "MERCHANT", Location: point})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
