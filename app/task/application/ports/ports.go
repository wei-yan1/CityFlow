// Package ports holds the narrow read capabilities Task's application
// services consume from other bounded contexts. They are declared by the
// consumer, so the Task service never imports another service's repository.
package ports

import (
	"context"

	"cityflow/app/task/domain/rider"
	"cityflow/app/task/domain/task"
	"cityflow/app/task/repository"
	"cityflow/internal/common/location"
)

// Account is the identity projection Task needs. Identity owns accounts; Task
// reads only these fields over RPC and never imports Identity's domain model.
type Account struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Role      string `json:"role"`
	Status    string `json:"status"`
}

type AccountReader interface {
	GetAccount(context.Context, string) (*Account, error)
}

type TaskReader interface {
	Get(context.Context, string) (*task.Task, error)
}

type RiderReader interface {
	RiderForUser(context.Context, string) (*rider.Profile, error)
	GetRider(context.Context, string) (*rider.Profile, error)
}

type PositionReader interface {
	GetPosition(context.Context, string) (*location.Position, error)
}

// RiderTaskAccess is the task capability used by the rider workbench.
type RiderTaskAccess interface {
	ListForRider(ctx context.Context, riderID, status string, page repository.Page) ([]*task.Task, error)
	ListAll(ctx context.Context, status string, page repository.Page) ([]*task.Task, error)
	OpenPoolVisible(ctx context.Context, item *task.Task) (bool, error)
	CountActiveForRider(ctx context.Context, riderID string) (int, error)
	Get(ctx context.Context, id string) (*task.Task, error)
	Claim(ctx context.Context, id, riderID string) (*task.Task, error)
	UpdateStatus(ctx context.Context, id, next, actorID, role string) (*task.Task, error)
	AdvancePickup(ctx context.Context, id, actor string, index int, next string) (*task.Task, error)
}
