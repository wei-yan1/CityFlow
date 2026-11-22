package location

import "time"

// TrustedPlace is a provider-originated POI with its verification window. It
// is a shared geo value, not an order/user domain object: Discovery writes it
// and Task reads it through the owning service.
type TrustedPlace struct {
	Place      Place
	VerifiedAt time.Time
	ExpiresAt  time.Time
}
