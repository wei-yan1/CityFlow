package repository

import (
	"context"
	"time"

	"cityflow/internal/domain/location"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
)

type AuthRepository interface {
	Authenticate(context.Context, string, string) (*user.User, error)
	GetUser(context.Context, string) (*user.User, error)
	IsActive(context.Context, string) (bool, error)
}

type Page struct {
	Limit  int
	Offset int
}

type UserRepository interface {
	CreateUser(context.Context, user.CreateRequest) (*user.User, error)
	UserByEmail(context.Context, string) (*user.User, error)
	UpdatePassword(context.Context, string, string) error
	CreatePasswordReset(context.Context, user.PasswordReset) error
	PasswordReset(context.Context, string) (*user.PasswordReset, error)
	DeletePasswordReset(context.Context, string) error
	ListUsers(context.Context, string, Page) ([]*user.User, error)
	UpdateUser(context.Context, string, user.UpdateRequest) (*user.User, error)
	SetUserStatus(context.Context, string, string) (*user.User, error)
}

type MerchantRepository interface {
	MapMerchants(context.Context, location.Bounds, int) ([]location.Place, error)
	MerchantForUser(context.Context, string) (*merchant.Profile, error)
	GetMerchant(context.Context, string) (*merchant.Profile, error)
	FindMerchantByAMapPOI(context.Context, string) (*merchant.Profile, error)
	FindMerchantNearName(context.Context, string, location.Point, float64) (*merchant.Profile, error)
	ListMerchants(context.Context, string, Page) ([]*merchant.Profile, error)
	CreateMerchant(context.Context, string, merchant.UpdateRequest) (*merchant.Profile, error)
	UpdateMerchant(context.Context, string, merchant.UpdateRequest) (*merchant.Profile, error)
	ArchiveMerchant(context.Context, string) error
}

type RiderRepository interface {
	RiderForUser(context.Context, string) (*rider.Profile, error)
	GetRider(context.Context, string) (*rider.Profile, error)
	ListRiders(context.Context, string, Page) ([]*rider.Profile, error)
	CreateRider(context.Context, string, rider.UpdateRequest) (*rider.Profile, error)
	UpdateRider(context.Context, string, rider.UpdateRequest) (*rider.Profile, error)
	SetRiderStatus(context.Context, string, string) (*rider.Profile, error)
	ArchiveRider(context.Context, string) error
}

// RiderRegistrationRepository owns paid rider applications. Approval creates
// both the login account and the rider profile in one transaction.
type RiderRegistrationRepository interface {
	CreateRiderRegistration(context.Context, rider.Registration) (*rider.Registration, error)
	RiderRegistrationExists(context.Context, string, string) (bool, error)
	ListRiderRegistrations(context.Context, string, Page) ([]*rider.Registration, error)
	GetRiderRegistration(context.Context, string) (*rider.Registration, error)
	ApproveRiderRegistration(context.Context, string, string, string) (*rider.Registration, error)
	RejectRiderRegistration(context.Context, string, string, string) (*rider.Registration, error)
}

type TaskRepository interface {
	Create(context.Context, task.CreateRequest, string) (*task.Task, error)
	FindIdempotent(context.Context, string, string, string) (*task.Task, error)
	List(context.Context, string, Page) ([]*task.Task, error)
	ListByCreator(context.Context, string, string, Page) ([]*task.Task, error)
	ListForRider(context.Context, string, string, Page) ([]*task.Task, error)
	Get(context.Context, string) (*task.Task, error)
	Update(context.Context, string, task.UpdateRequest) (*task.Task, error)
	Claim(context.Context, string, string) (*task.Task, error)
	AdvancePickup(context.Context, string, string, int, string) (*task.Task, error)
	UpdateStatus(context.Context, string, string, string) (*task.Task, error)
	CountActiveForRider(context.Context, string) (int, error)
	CreateIncident(context.Context, task.Incident) (*task.Incident, error)
	ListIncidents(context.Context, string) ([]task.Incident, error)
	ListIncidentsByStatus(context.Context, string, Page) ([]task.Incident, error)
	ResolveIncident(context.Context, string, string, string, string, time.Time) (*task.Incident, error)
}

// PositionRepository stores recent snapshots plus a rider-centric Redis track.
// Track samples are temporary; they are not duplicated per task in MySQL.
type PositionRepository interface {
	SavePosition(context.Context, location.Position, time.Duration) (*location.Position, error)
	GetPosition(context.Context, string) (*location.Position, error)
	NearbyPositions(context.Context, location.Point, float64, int) ([]location.Position, error)
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
