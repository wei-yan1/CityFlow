package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
)

const ReportInterval = 10 * time.Second

type riderProfileCacheEntry struct {
	profile *rider.Profile
	expires time.Time
}

type TrackingService struct {
	positions repository.PositionRepository
	events    repository.PositionEvents
	tracks    repository.TrackReader
	tasks     taskReader
	riders    riderForUserReader

	riderMu    sync.Mutex
	riderCache map[string]riderProfileCacheEntry
	riderTTL   time.Duration
}

func (s *TrackingService) WithTracks(tracks repository.TrackReader) *TrackingService {
	s.tracks = tracks
	return s
}

// WithHistory is kept as an alias for existing callers; the store is now a
// rider-centric Redis/memory track reader rather than a task table.
func (s *TrackingService) WithHistory(tracks repository.TrackReader) *TrackingService {
	return s.WithTracks(tracks)
}

type trackWindow struct {
	riderID string
	start   time.Time
	end     time.Time
}

func taskTrackWindows(item *task.Task, now time.Time) []trackWindow {
	if item == nil {
		return nil
	}
	windows := make([]trackWindow, 0)
	var open *trackWindow
	for _, event := range item.Events {
		switch event.To {
		case task.StatusClaimed:
			// A new claim starts a new assignment window. If a previous claim is
			// still open, close it at the new claim time.
			if open != nil {
				open.end = event.CreatedAt
				if !open.start.IsZero() && !open.end.Before(open.start) && open.riderID != "" {
					windows = append(windows, *open)
				}
			}
			riderID := event.ActorID
			if riderID == "" {
				riderID = item.WorkerID
			}
			open = &trackWindow{riderID: riderID, start: event.CreatedAt}
		case task.StatusCompleted, task.StatusCancelled:
			if open != nil {
				open.end = event.CreatedAt
				if !open.start.IsZero() && !open.end.Before(open.start) && open.riderID != "" {
					windows = append(windows, *open)
				}
				open = nil
			}
		}
	}
	if open != nil {
		open.end = now
		if !open.start.IsZero() && !open.end.Before(open.start) && open.riderID != "" {
			windows = append(windows, *open)
		}
	}
	if len(windows) == 0 && item.WorkerID != "" {
		start := item.CreatedAt
		if start.IsZero() {
			start = item.UpdatedAt
		}
		end := now
		if item.Status == task.StatusCompleted || item.Status == task.StatusCancelled {
			end = item.UpdatedAt
		}
		if !start.IsZero() && !end.Before(start) {
			windows = append(windows, trackWindow{riderID: item.WorkerID, start: start, end: end})
		}
	}
	return windows
}

func (s *TrackingService) History(ctx context.Context, id string, claims auth.Claims, after uint64, limit int) ([]location.TrackPoint, error) {
	if s.tasks == nil {
		return nil, domain.ErrUnavailable
	}
	item, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// The REST gateway asks for limit+1 to compute has_more; 501 is the
	// internal fetch ceiling, while the public API still caps user limit at 500.
	if limit < 1 || limit > 501 {
		return nil, domain.ErrInvalidRequest
	}
	if s.tracks == nil {
		return nil, domain.ErrUnavailable
	}
	windows := taskTrackWindows(item, time.Now().UTC())
	if len(windows) == 0 {
		// No rider has ever been assigned, so there is no track to protect.
		// Returning an empty result lets every authenticated role distinguish
		// "no track yet" from an authorization failure.
		return []location.TrackPoint{}, nil
	}
	allowed := claims.Role == auth.RoleAdmin ||
		(claims.Role == auth.RoleUser && item.CreatedBy == claims.Subject)
	if claims.Role == auth.RoleRider {
		for _, window := range windows {
			if window.riderID == claims.Subject {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return nil, domain.ErrForbidden
	}

	values := make([]location.TrackPoint, 0, limit)
	for _, window := range windows {
		if window.riderID == "" || window.start.IsZero() || window.end.IsZero() || window.end.Before(window.start) {
			continue
		}
		points, err := s.tracks.RiderTrack(ctx, window.riderID, window.start, window.end, after, limit)
		if err != nil {
			return nil, fmt.Errorf("%w: history query failed", domain.ErrUnavailable)
		}
		for _, point := range points {
			point.TaskID = id
			point.Position.TaskID = id
			if point.ID == 0 {
				point.ID = uint64(point.ObservedAt.UnixMilli())
			}
			values = append(values, point)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].ObservedAt.Equal(values[j].ObservedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].ObservedAt.Before(values[j].ObservedAt)
	})
	result := values[:0]
	var lastID uint64
	for _, point := range values {
		if point.ID == lastID {
			continue
		}
		result = append(result, point)
		lastID = point.ID
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func NewTrackingService(positions repository.PositionRepository, events repository.PositionEvents, tasks taskReader, riders riderForUserReader) *TrackingService {
	return &TrackingService{
		positions:  positions,
		events:     events,
		tasks:      tasks,
		riders:     riders,
		riderCache: make(map[string]riderProfileCacheEntry),
		riderTTL:   5 * time.Second,
	}
}

func (s *TrackingService) Report(ctx context.Context, id string, input location.Report) (*location.Position, error) {
	if s.positions == nil {
		return nil, domain.ErrUnavailable
	}
	now := time.Now().UTC()
	if input.Source == "" {
		input.Source = "device"
	}
	if input.Source != "device" && input.Source != "simulated" {
		return nil, fmt.Errorf("%w: invalid location source", domain.ErrInvalidRequest)
	}
	if !input.Point.Valid() || math.Abs(input.Latitude) > 85.05112878 || math.IsNaN(input.Accuracy) || math.IsInf(input.Accuracy, 0) || input.Accuracy < 0 || input.Accuracy > 10000 || input.ObservedAt.Before(now.Add(-2*time.Minute)) || input.ObservedAt.After(now.Add(30*time.Second)) {
		return nil, fmt.Errorf("%w: invalid coordinates, accuracy or observation time", domain.ErrInvalidRequest)
	}
	profile, err := s.riderProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	if profile.Status == "SUSPENDED" {
		return nil, domain.ErrForbidden
	}
	p, err := s.positions.SavePosition(ctx, location.Position{
		RiderID: id, Source: input.Source, TaskID: input.TaskID,
		Point: input.Point, Accuracy: input.Accuracy, ObservedAt: input.ObservedAt, UpdatedAt: now,
	}, ReportInterval)
	if err != nil {
		if errors.Is(err, domain.ErrRateLimited) || errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	if s.events != nil {
		s.events.Publish(*p)
	}
	return p, nil
}

func (s *TrackingService) riderProfile(ctx context.Context, userID string) (*rider.Profile, error) {
	if userID == "" {
		return nil, domain.ErrNotFound
	}
	now := time.Now()
	s.riderMu.Lock()
	if entry, ok := s.riderCache[userID]; ok && now.Before(entry.expires) {
		profile := entry.profile
		s.riderMu.Unlock()
		return profile, nil
	}
	s.riderMu.Unlock()
	if s.riders == nil {
		return nil, domain.ErrUnavailable
	}
	profile, err := s.riders.RiderForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	cached := *profile
	s.riderMu.Lock()
	s.riderCache[userID] = riderProfileCacheEntry{profile: &cached, expires: now.Add(s.riderTTL)}
	s.riderMu.Unlock()
	return &cached, nil
}

// CanTrack resolves authorization from server-side task ownership, never from a channel name.
func (s *TrackingService) CanTrack(ctx context.Context, id string, claims auth.Claims) (string, error) {
	if s.tasks == nil {
		return "", domain.ErrUnavailable
	}
	item, err := s.tasks.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if claims.Role != auth.RoleAdmin && !(claims.Role == auth.RoleUser && item.CreatedBy == claims.Subject) && !(claims.Role == auth.RoleRider && item.WorkerID == claims.Subject) {
		return "", domain.ErrForbidden
	}
	if item.WorkerID == "" || item.Status == task.StatusOpen || item.Status == task.StatusCompleted || item.Status == task.StatusCancelled {
		return "", domain.ErrNotFound
	}
	profile, err := s.riderProfile(ctx, item.WorkerID)
	if err != nil {
		return "", err
	}
	if profile.Status == "SUSPENDED" {
		return "", domain.ErrNotFound
	}
	return item.WorkerID, nil
}

func (s *TrackingService) Snapshot(ctx context.Context, id string, claims auth.Claims) (*location.Position, error) {
	riderID, err := s.CanTrack(ctx, id, claims)
	if err != nil {
		return nil, err
	}
	p, err := s.positions.GetPosition(ctx, riderID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	return p, err
}

func (s *TrackingService) Subscribe(id string) (<-chan location.Position, func()) {
	return s.events.Subscribe(id)
}

func (s *TrackingService) Nearby(ctx context.Context, center location.Point, radius float64, limit int) ([]location.Position, error) {
	if !center.Valid() || math.Abs(center.Latitude) > 85.05112878 || math.IsNaN(radius) || math.IsInf(radius, 0) || radius <= 0 || radius > 50000 || limit < 1 || limit > 100 {
		return nil, domain.ErrInvalidRequest
	}
	values, err := s.positions.NearbyPositions(ctx, center, radius, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	result := make([]location.Position, 0, len(values))
	for _, p := range values {
		profile, err := s.riderProfile(ctx, p.RiderID)
		if err == nil && profile.Status != "SUSPENDED" {
			result = append(result, p)
		} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	return result, nil
}
