package contract

import (
	"context"
)

// Reaction target kinds are stable wire contracts shared by the Reaction
// service, the content services and the Gateway. No content service may own
// like state; those constants only describe the objects being reacted to.
// Merchant likes are not part of this generic contract: Discovery owns the
// monthly merchant fact table and hot ranking.
const (
	KindLike     = "LIKE"
	KindFavorite = "FAVORITE"

	TargetPlace        = "PLACE"
	TargetPlaceReview  = "PLACE_REVIEW"
	TargetPlaceComment = "PLACE_COMMENT"
	TargetRiderReview  = "RIDER_REVIEW"
	TargetRiderComment = "RIDER_COMMENT"
)

// ReactionState is the read-only projection used by review DTOs. Count is the
// absolute reaction count; Active means the requesting user currently reacted.
type ReactionState struct {
	Count  int64
	Active bool
}

func ValidReactionTargetType(value string) bool {
	switch value {
	case TargetPlace, TargetPlaceReview, TargetPlaceComment, TargetRiderReview, TargetRiderComment:
		return true
	default:
		return false
	}
}

func ValidReactionKind(value string) bool {
	return value == KindLike || value == KindFavorite
}

// ReactionReader is implemented by the Reaction RPC adapter. Read models never
// write reaction records; they only consume this projection.
type ReactionReader interface {
	ReactionStates(ctx context.Context, targetType string, ids []string, kind, userID string) (map[string]ReactionState, error)
	// ReactionLiked returns only the requesting user's active flags. Comment
	// lists use this instead of ReactionStates so the displayed count comes
	// from each content service's like_count projection.
	ReactionLiked(ctx context.Context, targetType string, ids []string, kind, userID string) (map[string]bool, error)
}
