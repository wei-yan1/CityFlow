// Package trackingrepository defines the persistence ports owned by Tracking.
package repository

import (
	"context"
	"time"

	"cityflow/internal/common/location"
	"cityflow/internal/common/pagination"
)

// PositionRepository stores recent snapshots plus a rider-centric Redis track.
// Track samples are queued in Redis only until the MQ archive consumer has
// persisted them to MySQL.
type PositionRepository interface {
	SavePosition(context.Context, location.Position, time.Duration) (*location.Position, error)
	GetPosition(context.Context, string) (*location.Position, error)
	NearbyPositions(context.Context, location.Point, float64, int) ([]location.Position, error)
}

// TrackBatch is a page of pending rider samples read from Redis. Members are
// the exact ZSET payloads so the consumer can remove only the archived points.
type TrackBatch struct {
	RiderID     string
	Points      []location.TrackPoint
	Members     []string
	LastVersion uint64
}

// TrackQueue is Tracking's Redis-side pending queue and archiver lock.
type TrackQueue interface {
	PendingRiders(context.Context) ([]string, error)
	BackfillPendingRiders(context.Context) error
	PendingTrackBatch(context.Context, string, uint64, int) (TrackBatch, error)
	TrackCursor(context.Context, string) (uint64, error)
	SetTrackCursor(context.Context, string, uint64) error
	RemoveTrackMembers(context.Context, string, []string) error
	ForgetRiderIfEmpty(context.Context, string) (bool, error)
	AcquireArchiverLock(context.Context, time.Duration) (string, bool, error)
	ReleaseArchiverLock(context.Context, string) error
}

// TrackArchive is the durable MySQL trajectory archive. A batch insert is
// idempotent on (rider_id, version), so MQ redelivery is safe.
type TrackArchive interface {
	TrackReader
	ArchiveTrackPoints(context.Context, []location.TrackPoint) error
}

type PositionEvents interface {
	Publish(location.Position)
	Subscribe(string) (<-chan location.Position, func())
}

// TrackReader reads rider-centric trajectory samples. Samples are stored per
// rider, not per task; task ownership is resolved by the caller from task
// events / assignment windows.
type TrackReader interface {
	RiderTrack(context.Context, string, time.Time, time.Time, uint64, int) ([]location.TrackPoint, error)
}

// TrackRepository is the optional maintenance surface for rider trajectories.
type TrackRepository interface {
	TrackReader
	DeleteRiderTrack(context.Context, string) error
}

// PositionJanitor removes stale GEO/ZSET members outside the report hot path.
type PositionJanitor interface {
	CleanupStalePositions(context.Context, int) (int, error)
}

type Page = pagination.Page
