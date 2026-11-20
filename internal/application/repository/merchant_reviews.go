package repository

import (
	"context"

	"cityflow/internal/domain/merchant"
)

type MerchantReviews interface {
	CreatePlaceReview(context.Context, merchant.Review) error
	DeletePlaceReview(context.Context, string, string) error
	PlaceReviews(context.Context, string, string, string, int, int) ([]merchant.Review, int, float64, error)
}

// PublicPlaceReviewReader is the public projection. Implementations must not
// return order_id and should only select user_id to compute Mine.
type PublicPlaceReviewReader interface {
	PublicPlaceReviews(context.Context, string, string, string, int, int) ([]merchant.Review, int, float64, error)
}

// PlaceReviewAdmin is the privileged projection used only by the platform
// review list and the audited reveal endpoint.
type PlaceReviewAdmin interface {
	ListPlaceReviewsAdmin(context.Context, string, string, int, int) ([]merchant.Review, int, error)
	RevealPlaceReviewIdentity(context.Context, string, string, string) (string, error)
}

// PlaceReviewLikeProjection is the only write Discovery performs for likes:
// Reaction publishes the absolute count and Discovery updates its read model.
type PlaceReviewLikeProjection interface {
	UpdatePlaceReviewLikeCount(context.Context, string, int64) error
}
