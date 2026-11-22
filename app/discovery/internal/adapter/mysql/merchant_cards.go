package discoverymysql

import (
	"context"
	"fmt"
	"time"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/app/discovery/repository"
)

// MerchantAvatars loads every non-empty avatar for the requested merchants in
// one statement. Empty links are treated as absent so the UI owns the fallback.
func (s *Store) MerchantAvatars(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT merchant_id, avatar_url FROM merchant_media
		 WHERE merchant_id IN (`+placeholders(len(ids))+`) AND avatar_url <> ''`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("load merchant avatars: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var merchantID, avatar string
		if err := rows.Scan(&merchantID, &avatar); err != nil {
			return nil, fmt.Errorf("scan merchant avatar: %w", err)
		}
		out[merchantID] = avatar
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant avatars: %w", err)
	}
	return out, nil
}

// MerchantTopReviews returns one highest-liked review per merchant plus that
// merchant's review count and average rating. Window functions keep the whole
// projection to a single query instead of one review lookup per card.
func (s *Store) MerchantTopReviews(ctx context.Context, ids []string) (map[string]repository.MerchantReviewCard, error) {
	out := make(map[string]repository.MerchantReviewCard, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT place_id,id,COALESCE(author_label,''),anonymous,rating,comment,like_count,created_at,review_count,avg_rating
		 FROM (
		   SELECT r.place_id,r.id,r.author_label,r.anonymous,r.rating,r.comment,r.like_count,r.created_at,
		          ROW_NUMBER() OVER (PARTITION BY r.place_id ORDER BY r.like_count DESC,r.created_at DESC,r.id DESC) AS row_num,
		          COUNT(*) OVER (PARTITION BY r.place_id) AS review_count,
		          AVG(r.rating) OVER (PARTITION BY r.place_id) AS avg_rating
		   FROM place_reviews r
		   WHERE r.place_source='merchant' AND r.place_id IN (`+placeholders(len(ids))+`)
		 ) ranked
		 WHERE row_num=1`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("load merchant top reviews: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var placeID, reviewID, authorLabel, comment string
		var anonymous bool
		var rating, likes, count int
		var created time.Time
		var average float64
		if err := rows.Scan(&placeID, &reviewID, &authorLabel, &anonymous, &rating, &comment, &likes, &created, &count, &average); err != nil {
			return nil, fmt.Errorf("scan merchant top review: %w", err)
		}
		out[placeID] = repository.MerchantReviewCard{
			Review: merchant.Review{
				ID: reviewID, AuthorLabel: authorLabel, Anonymous: anonymous,
				Rating: rating, Comment: comment, Likes: likes, CreatedAt: created,
			},
			ReviewCount: count,
			Rating:      average,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant top reviews: %w", err)
	}
	return out, nil
}
