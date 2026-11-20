package repository

import (
	"context"

	"cityflow/internal/domain/comment"
)

// ReviewComments is implemented once per owning service. targetID is
// place_reviews.id for Discovery and rider_feedback.id for Task.
type ReviewComments interface {
	ReviewExists(context.Context, string) (bool, error)
	CreateComment(context.Context, comment.Record) error
	GetComment(context.Context, string, string) (comment.Record, error)
	ListTopComments(context.Context, string, string, int) ([]comment.Record, string, error)
	ListCommentThread(context.Context, string, string, int) ([]comment.Record, error)
	HideComment(context.Context, string, string) error
}
