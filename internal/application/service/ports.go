package service

import (
	"context"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
)

// Narrow read ports for data owned by another service. A monolith satisfies
// them with its shared store; a microservice satisfies them with an RPC
// adapter. Keeping them minimal is what lets a service read a neighbour's
// facts without touching the neighbour's tables.

type accountReader interface {
	GetUser(ctx context.Context, id string) (*user.User, error)
}

type taskReader interface {
	Get(ctx context.Context, id string) (*task.Task, error)
}

type taskReadLister interface {
	Get(ctx context.Context, id string) (*task.Task, error)
	List(ctx context.Context, status string, page repository.Page) ([]*task.Task, error)
}

type riderForUserReader interface {
	RiderForUser(ctx context.Context, userID string) (*rider.Profile, error)
}

type riderReader interface {
	RiderForUser(ctx context.Context, userID string) (*rider.Profile, error)
	GetRider(ctx context.Context, id string) (*rider.Profile, error)
}

type positionReader interface {
	GetPosition(ctx context.Context, riderID string) (*location.Position, error)
}
