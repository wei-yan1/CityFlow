// Package notification owns in-app messages and their read state.
package notification

import (
	"errors"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid notification request")
	ErrNotFound       = errors.New("notification not found")
	ErrUnavailable    = errors.New("notification unavailable")
)

type Notification struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	TaskID    string     `json:"task_id,omitempty"`
	EventID   string     `json:"event_id,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (n Notification) Read() bool { return n.ReadAt != nil }
