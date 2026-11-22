// Package identityrepository defines the persistence ports owned by Identity.
package repository

import (
	"context"
	"time"

	identityrider "cityflow/app/identity/domain/rider"
	"cityflow/app/identity/domain/user"
	"cityflow/internal/common/pagination"
)

type AuthRepository interface {
	Authenticate(context.Context, string, string) (*user.User, error)
	GetUser(context.Context, string) (*user.User, error)
	IsActive(context.Context, string) (bool, error)
}

type UserRepository interface {
	CreateUser(context.Context, user.CreateRequest) (*user.User, error)
	UserByEmail(context.Context, string) (*user.User, error)
	UpdatePassword(context.Context, string, string) error
	CreatePasswordReset(context.Context, user.PasswordReset) error
	PasswordReset(context.Context, string) (*user.PasswordReset, error)
	DeletePasswordReset(context.Context, string) error
	PurgePasswordResets(context.Context, time.Time, int) (int64, error)
	ListUsers(context.Context, string, pagination.Page) ([]*user.User, error)
	UpdateUser(context.Context, string, user.UpdateRequest) (*user.User, error)
	SetUserStatus(context.Context, string, string) (*user.User, error)
}

// RiderRegistrationRepository owns paid rider applications. Approval creates
// the login account and publishes rider.approved.v1; Task creates the rider
// performance profile from that event.
type RiderRegistrationRepository interface {
	CreateRiderRegistration(context.Context, identityrider.Registration) (*identityrider.Registration, error)
	RiderRegistrationExists(context.Context, string, string) (bool, error)
	ListRiderRegistrations(context.Context, string, pagination.Page) ([]*identityrider.Registration, error)
	GetRiderRegistration(context.Context, string) (*identityrider.Registration, error)
	ApproveRiderRegistration(context.Context, string, string, string) (*identityrider.Registration, error)
	RejectRiderRegistration(context.Context, string, string, string) (*identityrider.Registration, error)
}

type Page = pagination.Page
