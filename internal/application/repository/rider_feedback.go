package repository

import (
	"context"

	"cityflow/internal/domain/feedback"
)

// RiderFeedback is the transaction boundary for reviews, tips, calls and
// reports. Every record carries rider, task, user and creation time together.
type RiderFeedback interface {
	CreateRiderFeedback(context.Context, feedback.Record) error
	ListRiderFeedback(context.Context, string, []string, int, int) ([]feedback.Record, error)
	ListRiderReports(context.Context, string, Page) ([]feedback.Record, error)
	ResolveRiderReport(context.Context, string, string, string, string) error
}
