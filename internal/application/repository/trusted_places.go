package repository

import (
	"context"
	"time"

	"cityflow/internal/domain/location"
)

// TrustedPlace contains a provider result in GCJ02, together with its expiry.
// Only server-side provider responses may be passed to SaveTrustedPlaces.
type TrustedPlace struct {
	Place      location.Place
	VerifiedAt time.Time
	ExpiresAt  time.Time
}

type TrustedPlaces interface {
	SaveTrustedPlaces(context.Context, []TrustedPlace) error
	FindTrustedPlaces(context.Context, []string, time.Time) (map[string]TrustedPlace, error)
}
