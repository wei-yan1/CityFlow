// Package discoveryrepository defines the persistence ports owned by Discovery.
package repository

import (
	"context"
	"time"

	"cityflow/app/discovery/domain/comment"
	"cityflow/app/discovery/domain/merchant"
	"cityflow/internal/common/location"
	"cityflow/internal/common/pagination"
)

type MerchantRepository interface {
	MapMerchants(context.Context, location.Bounds, int) ([]location.Place, error)
	MerchantForUser(context.Context, string) (*merchant.Profile, error)
	GetMerchant(context.Context, string) (*merchant.Profile, error)
	GetMerchantsByIDs(context.Context, []string) ([]*merchant.Profile, error)
	GetMerchantsByIDsIncludingArchived(context.Context, []string) ([]*merchant.Profile, error)
	FindMerchantByAMapPOI(context.Context, string) (*merchant.Profile, error)
	FindMerchantNearName(context.Context, string, location.Point, float64) (*merchant.Profile, error)
	ListMerchants(context.Context, string, pagination.Page) ([]*merchant.Profile, error)
	CreateMerchant(context.Context, string, merchant.UpdateRequest) (*merchant.Profile, error)
	UpdateMerchant(context.Context, string, merchant.UpdateRequest) (*merchant.Profile, error)
	ArchiveMerchant(context.Context, string) error
}

// MerchantLikeCount is a merchant/month aggregate. It is used both for the
// Redis top-list entry and for rebuilding the ZSET from MySQL.
type MerchantLikeCount struct {
	MerchantID string
	LikeCount  int64
}

// MerchantHotSnapshotItem is one immutable rank slot in merchant_hot_snapshot.
// Rank starts at 1 and the snapshot survives merchant archival.
type MerchantHotSnapshotItem struct {
	Rank       int
	MerchantID string
	LikeCount  int64
}

// MerchantHotSnapshot persists the monthly Top N before Redis is rebuilt or
// disposed. Discovery owns this table; no other service writes it.
type MerchantHotSnapshot interface {
	ReplaceMerchantHotSnapshot(ctx context.Context, month, category string, items []MerchantHotSnapshotItem) error
	ListMerchantHotSnapshot(ctx context.Context, month, category string, limit int) ([]MerchantHotSnapshotItem, error)
}

// MerchantLikeTotals is the current user's own merchant-like ledger. Monthly
// counts the current bucket; Lifetime sums every month the user has liked.
type MerchantLikeTotals struct {
	Monthly  int64
	Lifetime int64
}

// MerchantLikes is the authoritative month-scoped like ledger. Redis is only a
// ranking projection and may be rebuilt from these rows.
type MerchantLikes interface {
	AddMerchantLike(context.Context, string, string, string) (bool, error)
	RemoveMerchantLike(context.Context, string, string, string) (bool, error)
	MerchantLikeCount(context.Context, string, string) (int64, error)
	MerchantLikeCounts(context.Context, string, []string) (map[string]int64, error)
	MerchantLikeUsers(context.Context, string, string, []string) (map[string]bool, error)
	MerchantLikeStats(context.Context, string, string) (MerchantLikeTotals, error)
	ListMerchantLikeCounts(context.Context, string, string, int) ([]MerchantLikeCount, error)
}

// MerchantHotRanking is the Redis read model for the real-time Top10. Scores
// are absolute monthly counts, never deltas, so a missed update is repaired by
// the next write for the same merchant.
type MerchantHotRanking interface {
	SetMerchantScore(context.Context, string, string, string, int64) error
	ClearMerchantScores(context.Context, string, string) error
	TopMerchantScores(context.Context, string, string, int) ([]MerchantLikeCount, error)
	GetMerchantProfiles(context.Context, []string) (map[string]*merchant.Profile, []string, error)
	CacheMerchantProfiles(context.Context, []*merchant.Profile, time.Duration) error
	InvalidateMerchantProfile(context.Context, string) error
	Ping(context.Context) error
}

type MerchantMedia interface {
	GetMerchantMedia(context.Context, string) (string, []string, error)
	SaveMerchantMedia(context.Context, string, string, []string) error
}

// MerchantReviewCard is the single review a public ranking card needs. It is
// loaded for many merchants in one query so a hot list never becomes N+1.
type MerchantReviewCard struct {
	Review      merchant.Review
	ReviewCount int
	Rating      float64
}

// MerchantCardExtras is the read-only batch projection used by the monthly
// hot list. It must not be used for merchant-owned or admin review views.
type MerchantCardExtras interface {
	MerchantAvatars(context.Context, []string) (map[string]string, error)
	MerchantTopReviews(context.Context, []string) (map[string]MerchantReviewCard, error)
}

type MerchantReviews interface {
	CreatePlaceReview(context.Context, merchant.Review) error
	DeletePlaceReview(context.Context, string, string) error
	PlaceReviews(context.Context, string, string, string, int, int) ([]merchant.Review, int, float64, error)
}

// PublicPlaceReviewReader is the public projection. Implementations must not
// return task_id and should only select user_id to compute Mine.
type PublicPlaceReviewReader interface {
	PublicPlaceReviews(context.Context, string, string, string, int, int) ([]merchant.Review, int, float64, error)
}

// PlaceReviewAdmin is the privileged projection used only by the platform
// review list and the audited reveal endpoint.
type PlaceReviewAdmin interface {
	ListPlaceReviewsAdmin(context.Context, string, string, int, int) ([]merchant.Review, int, error)
	RevealPlaceReviewIdentity(context.Context, string, string, string) (string, error)
}

// LikeProjection is the only like write Discovery performs: Reaction owns
// state, this service only stores the absolute count pushed by MQ.
type LikeProjection interface {
	UpdateLikeCounts(context.Context, string, map[string]int64) error
}

// ReviewComments is implemented for place_reviews inside Discovery.
type ReviewComments interface {
	ReviewExists(context.Context, string) (bool, error)
	CreateComment(context.Context, comment.Record) error
	GetComment(context.Context, string, string) (comment.Record, error)
	ListTopComments(context.Context, string, string, string, int) ([]comment.Record, string, error)
	ListCommentThread(context.Context, string, string, int) ([]comment.Record, error)
	HideComment(context.Context, string, string) error
}

// TrustedPlace is the shared geo value re-exported for Discovery adapters.
type TrustedPlace = location.TrustedPlace

type TrustedPlaces interface {
	SaveTrustedPlaces(context.Context, []TrustedPlace) error
	FindTrustedPlaces(context.Context, []string, time.Time) (map[string]TrustedPlace, error)
}

// TrustedPlacePurger is the optional maintenance surface for expired POI
// verification rows. Reads already filter by expires_at; this only reclaims
// rows whose POI is never searched again.
type TrustedPlacePurger interface {
	PurgeTrustedPlaces(context.Context, time.Time, int) (int64, error)
}

type Page = pagination.Page
