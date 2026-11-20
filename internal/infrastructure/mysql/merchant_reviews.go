package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"cityflow/internal/domain"
	"cityflow/internal/domain/merchant"
)

// UpdatePlaceReviewLikeCount applies the Reaction projection. Reaction owns
// the truth; Discovery only stores the denormalized display/sort value.
func (s *Store) UpdatePlaceReviewLikeCount(ctx context.Context, id string, count int64) error {
	if count < 0 {
		count = 0
	}
	_, err := s.db.ExecContext(ctx, `UPDATE place_reviews SET like_count=? WHERE id=?`, count, id)
	return err
}

func (s *Store) CreatePlaceReview(ctx context.Context, r merchant.Review) error {
	var orderID any
	if r.OrderID != "" {
		orderID = r.OrderID
	}
	_, e := s.db.ExecContext(ctx, `INSERT INTO place_reviews (id,order_id,place_source,place_id,user_id,anonymous,author_label,rating,comment) VALUES (?,?,?,?,?,?,?,?,?)`, r.ID, orderID, r.PlaceSource, r.PlaceID, r.UserID, r.Anonymous, r.AuthorLabel, r.Rating, r.Comment)
	if e != nil {
		return writeError(e)
	}
	return nil
}
func (s *Store) DeletePlaceReview(ctx context.Context, id, user string) error {
	res, e := s.db.ExecContext(ctx, `DELETE FROM place_reviews WHERE id=? AND user_id=?`, id, user)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	var author string
	e = s.db.QueryRowContext(ctx, `SELECT user_id FROM place_reviews WHERE id=?`, id).Scan(&author)
	if errors.Is(e, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if e != nil {
		return e
	}
	return domain.ErrForbidden
}
func (s *Store) PlaceReviews(ctx context.Context, source, id, user string, limit, offset int) ([]merchant.Review, int, float64, error) {
	var count int
	var avg float64
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(AVG(rating),0) FROM place_reviews WHERE place_source=? AND place_id=?`, source, id).Scan(&count, &avg); e != nil {
		return nil, 0, 0, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT r.id,COALESCE(r.order_id,''),r.user_id,r.anonymous,r.author_label,r.rating,r.comment,r.like_count,r.created_at FROM place_reviews r WHERE r.place_source=? AND r.place_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT ? OFFSET ?`, source, id, limit, offset)
	if e != nil {
		return nil, 0, 0, e
	}
	defer rows.Close()
	out := make([]merchant.Review, 0)
	for rows.Next() {
		var r merchant.Review
		if e := rows.Scan(&r.ID, &r.OrderID, &r.UserID, &r.Anonymous, &r.AuthorLabel, &r.Rating, &r.Comment, &r.Likes, &r.CreatedAt); e != nil {
			return nil, 0, 0, e
		}
		r.Mine = r.UserID == user
		out = append(out, r)
	}
	if e := rows.Err(); e != nil {
		return nil, 0, 0, e
	}
	if e := rows.Close(); e != nil {
		return nil, 0, 0, e
	}
	if len(out) == 0 {
		return out, count, avg, nil
	}
	return out, count, avg, nil
}

// PublicPlaceReviews never selects order_id. It selects user_id only to
// compute Mine; the service converts that flag into a DTO before JSON is sent.
func (s *Store) PublicPlaceReviews(ctx context.Context, source, id, user string, limit, offset int) ([]merchant.Review, int, float64, error) {
	var count int
	var avg float64
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(AVG(rating),0) FROM place_reviews WHERE place_source=? AND place_id=?`, source, id).Scan(&count, &avg); e != nil {
		return nil, 0, 0, e
	}
	rows, e := s.db.QueryContext(ctx,
		`SELECT r.id,r.user_id,r.anonymous,r.author_label,r.rating,r.comment,r.like_count,r.created_at
		 FROM place_reviews r
		 WHERE r.place_source=? AND r.place_id=?
		 ORDER BY r.like_count DESC,r.id DESC LIMIT ? OFFSET ?`,
		source, id, limit, offset)
	if e != nil {
		return nil, 0, 0, e
	}
	defer rows.Close()
	out := make([]merchant.Review, 0)
	for rows.Next() {
		var r merchant.Review
		if e := rows.Scan(&r.ID, &r.UserID, &r.Anonymous, &r.AuthorLabel, &r.Rating, &r.Comment, &r.Likes, &r.CreatedAt); e != nil {
			return nil, 0, 0, e
		}
		r.Mine = r.UserID == user
		out = append(out, r)
	}
	if e := rows.Err(); e != nil {
		return nil, 0, 0, e
	}
	if e := rows.Close(); e != nil {
		return nil, 0, 0, e
	}
	if len(out) == 0 {
		return out, count, avg, nil
	}
	return out, count, avg, nil
}

func (s *Store) ListPlaceReviewsAdmin(ctx context.Context, source, placeID string, limit, offset int) ([]merchant.Review, int, error) {
	where := "1=1"
	args := make([]any, 0, 2)
	if source != "" {
		where += " AND place_source=?"
		args = append(args, source)
	}
	if placeID != "" {
		where += " AND place_id=?"
		args = append(args, placeID)
	}
	var count int
	if e := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM place_reviews WHERE "+where, args...).Scan(&count); e != nil {
		return nil, 0, e
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, e := s.db.QueryContext(ctx,
		`SELECT r.id,COALESCE(r.order_id,''),r.user_id,r.anonymous,r.author_label,r.rating,r.comment,r.created_at
		 FROM place_reviews r WHERE `+where+`
		 ORDER BY r.created_at DESC,r.id DESC LIMIT ? OFFSET ?`,
		queryArgs...)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := make([]merchant.Review, 0)
	for rows.Next() {
		var r merchant.Review
		if e := rows.Scan(&r.ID, &r.OrderID, &r.UserID, &r.Anonymous, &r.AuthorLabel, &r.Rating, &r.Comment, &r.CreatedAt); e != nil {
			return nil, 0, e
		}
		out = append(out, r)
	}
	if e := rows.Err(); e != nil {
		return nil, 0, e
	}
	return out, count, nil
}

// RevealPlaceReviewIdentity writes the audit row in the same transaction as the
// identity lookup. The caller only gets a user_id; name and phone are resolved
// by the BFF from Identity.
func (s *Store) RevealPlaceReviewIdentity(ctx context.Context, reviewID, adminID, reason string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM place_reviews WHERE id=?`, reviewID).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return "", err
	}
	var recent int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM review_identity_audit
		 WHERE admin_id=? AND action='REVEAL' AND created_at >= NOW(3) - INTERVAL 1 HOUR`,
		adminID,
	).Scan(&recent); err != nil {
		return "", err
	}
	if recent >= 20 {
		return "", fmt.Errorf("%w: 每小时最多查看 20 条评价的真实身份", domain.ErrConflict)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO review_identity_audit(review_id,admin_id,action,reason) VALUES(?,?,?,?)`,
		reviewID, adminID, "REVEAL", reason,
	); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}
