// Package taskrepository defines the persistence ports owned by Task.
package repository

import (
	"context"
	"time"

	"cityflow/app/task/domain/comment"
	"cityflow/app/task/domain/feedback"
	"cityflow/app/task/domain/rider"
	"cityflow/app/task/domain/task"
	"cityflow/internal/common/outbox"
	"cityflow/internal/common/pagination"
)

type RiderRepository interface {
	RiderForUser(context.Context, string) (*rider.Profile, error)
	GetRider(context.Context, string) (*rider.Profile, error)
	ListRiders(context.Context, string, pagination.Page) ([]*rider.Profile, error)
	CreateRider(context.Context, string, rider.UpdateRequest) (*rider.Profile, error)
	UpdateRider(context.Context, string, rider.UpdateRequest) (*rider.Profile, error)
	SetRiderStatus(context.Context, string, string) (*rider.Profile, error)
	ArchiveRider(context.Context, string) error
}

type TaskRepository interface {
	Create(context.Context, task.CreateRequest, string) (*task.Task, error)
	FindIdempotent(context.Context, string, string, string) (*task.Task, error)
	List(context.Context, string, pagination.Page) ([]*task.Task, error)
	ListByCreator(context.Context, string, string, pagination.Page) ([]*task.Task, error)
	ListForRider(context.Context, string, string, pagination.Page) ([]*task.Task, error)
	Get(context.Context, string) (*task.Task, error)
	Update(context.Context, string, task.UpdateRequest) (*task.Task, error)
	Claim(context.Context, string, string) (*task.Task, error)
	AdvancePickup(context.Context, string, string, int, string) (*task.Task, error)
	UpdateStatus(context.Context, string, string, string) (*task.Task, error)
	Cancel(context.Context, string, string, string) (*task.Task, error)
	CountActiveForRider(context.Context, string) (int, error)
	CreateIncident(context.Context, task.Incident) (*task.Incident, error)
	ListIncidents(context.Context, string) ([]task.Incident, error)
	ListIncidentsByStatus(context.Context, string, pagination.Page) ([]task.Incident, error)
	ResolveIncident(context.Context, string, string, string, string, time.Time) (*task.Incident, error)
}

// RiderFeedback is the transaction boundary for reviews, tips, calls and
// reports. Every record carries rider, task, user and creation time together.
type RiderFeedback interface {
	CreateRiderFeedback(context.Context, feedback.Record) error
	ListRiderFeedback(context.Context, string, []string, int, int) ([]feedback.Record, error)
	ListRiderReports(context.Context, string, pagination.Page) ([]feedback.Record, error)
	ResolveRiderReport(context.Context, string, string, string, string) error
}

// ReviewComments is implemented for rider_feedback inside Task.
type ReviewComments interface {
	ReviewExists(context.Context, string) (bool, error)
	CreateComment(context.Context, comment.Record) error
	GetComment(context.Context, string, string) (comment.Record, error)
	ListTopComments(context.Context, string, string, string, int) ([]comment.Record, string, error)
	ListCommentThread(context.Context, string, string, int) ([]comment.Record, error)
	HideComment(context.Context, string, string) error
}

// LikeProjection stores the absolute counts published by the Reaction service.
type LikeProjection interface {
	UpdateLikeCounts(context.Context, string, map[string]int64) error
}

// TaskOutboxRepository changes a task status and writes the corresponding
// integration event in one transaction.
type TaskOutboxRepository interface {
	OutboxStore
	UpdateStatusWithOutbox(context.Context, string, string, string) (*task.Task, error)
	CancelWithOutbox(context.Context, string, string, string) (*task.Task, error)
}

// OutboxStore is the relay view of Task's own outbox table.
type OutboxStore interface {
	ClaimPendingOutbox(context.Context, time.Time, int) ([]outbox.Event, error)
	MarkOutboxPublished(context.Context, string, time.Time) error
	MarkOutboxFailed(context.Context, string, string, time.Time) error
	PurgePublishedOutbox(context.Context, time.Time, int) (int64, error)
}

type Page = pagination.Page
