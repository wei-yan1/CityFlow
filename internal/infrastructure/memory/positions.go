package memory

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
)

const PositionTTL = 2 * time.Minute

type Positions struct {
	mu     sync.Mutex
	values map[string]location.Position
	tracks map[string][]location.TrackPoint
}

func NewPositions() *Positions {
	return &Positions{values: make(map[string]location.Position), tracks: make(map[string][]location.TrackPoint)}
}
func (s *Positions) SavePosition(ctx context.Context, p location.Position, interval time.Duration) (*location.Position, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.values[p.RiderID]
	now := time.Now().UTC()
	if ok && now.Sub(previous.UpdatedAt) < interval {
		return nil, domain.ErrRateLimited
	}
	if ok && !p.ObservedAt.After(previous.ObservedAt) {
		return nil, domain.ErrConflict
	}
	p.UpdatedAt = now
	p.Version = now.UnixMilli()
	if p.Version <= previous.Version {
		p.Version = previous.Version + 1
	}
	s.values[p.RiderID] = p
	s.tracks[p.RiderID] = append(s.tracks[p.RiderID], location.TrackPoint{
		ID: uint64(p.Version), TaskID: p.TaskID, Position: p,
	})
	return &p, nil
}

func (s *Positions) RiderTrack(ctx context.Context, riderID string, from, to time.Time, after uint64, limit int) ([]location.TrackPoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make([]location.TrackPoint, 0)
	for _, p := range s.tracks[riderID] {
		if !from.IsZero() && p.ObservedAt.Before(from) {
			continue
		}
		if !to.IsZero() && p.ObservedAt.After(to) {
			continue
		}
		if after > 0 && p.ID <= after {
			continue
		}
		values = append(values, p)
		if len(values) == limit {
			break
		}
	}
	return values, nil
}

func (s *Positions) DeleteRiderTrack(ctx context.Context, riderID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tracks, riderID)
	return nil
}
func (s *Positions) GetPosition(ctx context.Context, id string) (*location.Position, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.values[id]
	if !ok || time.Since(p.UpdatedAt) > PositionTTL {
		delete(s.values, id)
		return nil, domain.ErrNotFound
	}
	return &p, nil
}
func (s *Positions) NearbyPositions(ctx context.Context, center location.Point, radius float64, limit int) ([]location.Position, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]location.Position, 0)
	for id, p := range s.values {
		if time.Since(p.UpdatedAt) > PositionTTL {
			delete(s.values, id)
			continue
		}
		if distance(center, p.Point) <= radius {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool { return distance(center, result[i].Point) < distance(center, result[j].Point) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func distance(a, b location.Point) float64 {
	rad := math.Pi / 180
	dlat, dlon := (b.Latitude-a.Latitude)*rad, (b.Longitude-a.Longitude)*rad
	h := math.Pow(math.Sin(dlat/2), 2) + math.Cos(a.Latitude*rad)*math.Cos(b.Latitude*rad)*math.Pow(math.Sin(dlon/2), 2)
	return 6371000 * 2 * math.Asin(math.Sqrt(math.Min(1, h)))
}
