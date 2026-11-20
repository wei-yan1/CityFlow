// Package reaction owns durable like/favorite records. It never owns review
// text, comment text or user profiles.
package reaction

import (
	"fmt"
	"strings"
	"time"
)

const (
	TargetPlaceReview  = "PLACE_REVIEW"
	TargetRiderReview  = "RIDER_REVIEW"
	TargetMerchant     = "MERCHANT"
	TargetPlace        = "PLACE"
	TargetPlaceComment = "PLACE_COMMENT"
	TargetRiderComment = "RIDER_COMMENT"

	KindLike     = "LIKE"
	KindFavorite = "FAVORITE"
)

type Record struct {
	ID         uint64
	TargetType string
	TargetID   string
	UserID     string
	Kind       string
	CreatedAt  time.Time
}

type Op struct {
	TargetType string
	TargetID   string
	UserID     string
	Kind       string
	Active     bool
}

func ValidTargetType(value string) bool {
	switch value {
	case TargetPlaceReview, TargetRiderReview, TargetMerchant, TargetPlace, TargetPlaceComment, TargetRiderComment:
		return true
	default:
		return false
	}
}

func ValidKind(value string) bool {
	return value == KindLike || value == KindFavorite
}

// CanonicalTargetID is the single place that decides the wire format of a
// target id. Callers must not concatenate source and place id themselves.
func CanonicalTargetID(targetType, source, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("target id is required")
	}
	switch targetType {
	case TargetPlace:
		source = strings.TrimSpace(source)
		if source == "" {
			return "", fmt.Errorf("place target requires source and place id")
		}
		prefix := source + ":"
		if strings.HasPrefix(id, prefix) {
			return id, nil
		}
		return prefix + id, nil
	default:
		return id, nil
	}
}
