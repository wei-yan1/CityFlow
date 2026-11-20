// Package outbox contains the database-backed event record shared by services
// that publish integration events. A service writes an Event in the same local
// transaction as its business state, then a relay publishes it afterwards.
package outbox

import "time"

const (
	StatusPending    = "PENDING"
	StatusPublishing = "PUBLISHING"
	StatusPublished  = "PUBLISHED"
	StatusFailed     = "FAILED"
)

// Event is one durable outbox row. Payload is JSON; the relay copies its bytes
// into a messaging.Message without interpreting the service-specific schema.
type Event struct {
	ID          string
	Type        string
	AggregateID string
	Payload     []byte
	Status      string
	Attempts    int
	AvailableAt time.Time
	LockedUntil *time.Time
	PublishedAt *time.Time
	LastError   string
	CreatedAt   time.Time
}
