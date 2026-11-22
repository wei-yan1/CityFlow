package discoverymysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/internal/common/domain"

	"gorm.io/gorm"
)

func (s *Store) UpdateLikeCounts(ctx context.Context, targetType string, counts map[string]int64) error {
	if len(counts) == 0 {
		return nil
	}
	table := ""
	switch targetType {
	case "PLACE_REVIEW":
		table = "place_reviews"
	case "PLACE_COMMENT":
		table = placeCommentTable
	default:
		return nil
	}
	return s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for id, count := range counts {
			if id == "" {
				continue
			}
			if count < 0 {
				count = 0
			}
			if err := tx.Exec(`UPDATE `+table+` SET like_count=? WHERE id=?`, count, id).Error; err != nil {
				return fmt.Errorf("update %s like_count: %w", table, err)
			}
		}
		return nil
	})
}

func (s *Store) UpdatePlaceReviewLikeCount(ctx context.Context, id string, count int64) error {
	if count < 0 {
		count = 0
	}
	_, err := s.db.SQL().ExecContext(ctx, `UPDATE place_reviews SET like_count=? WHERE id=?`, count, id)
	return err
}

func (s *Store) CreatePlaceReview(ctx context.Context, r merchant.Review) error {
	var taskID any
	if r.TaskID != "" {
		taskID = r.TaskID
	}
	_, err := s.db.SQL().ExecContext(ctx, `INSERT INTO place_reviews (id,task_id,place_source,place_id,user_id,anonymous,author_label,rating,comment) VALUES (?,?,?,?,?,?,?,?,?)`, r.ID, taskID, r.PlaceSource, r.PlaceID, r.UserID, r.Anonymous, r.AuthorLabel, r.Rating, r.Comment)
	if err != nil {
		return writeError(err)
	}
	return nil
}

func (s *Store) DeletePlaceReview(ctx context.Context, id, user string) error {
	res, err := s.db.SQL().ExecContext(ctx, `DELETE FROM place_reviews WHERE id=? AND user_id=?`, id, user)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var author string
	err = s.db.SQL().QueryRowContext(ctx, `SELECT user_id FROM place_reviews WHERE id=?`, id).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	return domain.ErrForbidden
}

func (s *Store) PlaceReviews(ctx context.Context, source, id, user string, limit, offset int) ([]merchant.Review, int, float64, error) {
	var count int
	var avg float64
	if err := s.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(AVG(rating),0) FROM place_reviews WHERE place_source=? AND place_id=?`, source, id).Scan(&count, &avg); err != nil {
		return nil, 0, 0, err
	}
	rows, err := s.db.SQL().QueryContext(ctx, `SELECT r.id,COALESCE(r.task_id,''),r.user_id,r.anonymous,r.author_label,r.rating,r.comment,r.like_count,r.created_at FROM place_reviews r WHERE r.place_source=? AND r.place_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT ? OFFSET ?`, source, id, limit, offset)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	out := make([]merchant.Review, 0)
	for rows.Next() {
		var r merchant.Review
		if err := rows.Scan(&r.ID, &r.TaskID, &r.UserID, &r.Anonymous, &r.AuthorLabel, &r.Rating, &r.Comment, &r.Likes, &r.CreatedAt); err != nil {
			return nil, 0, 0, err
		}
		r.Mine = r.UserID == user
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	return out, count, avg, nil
}

// PublicPlaceReviews never selects task_id; user_id is used only to compute
// the per-request Mine flag before the DTO leaves the service.
func (s *Store) PublicPlaceReviews(ctx context.Context, source, id, user string, limit, offset int) ([]merchant.Review, int, float64, error) {
	var count int
	var avg float64
	if err := s.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(AVG(rating),0) FROM place_reviews WHERE place_source=? AND place_id=?`, source, id).Scan(&count, &avg); err != nil {
		return nil, 0, 0, err
	}
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT r.id,r.user_id,r.anonymous,r.author_label,r.rating,r.comment,r.like_count,r.created_at
		 FROM place_reviews r
		 WHERE r.place_source=? AND r.place_id=?
		 ORDER BY r.like_count DESC,r.id DESC LIMIT ? OFFSET ?`,
		source, id, limit, offset)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	out := make([]merchant.Review, 0)
	for rows.Next() {
		var r merchant.Review
		if err := rows.Scan(&r.ID, &r.UserID, &r.Anonymous, &r.AuthorLabel, &r.Rating, &r.Comment, &r.Likes, &r.CreatedAt); err != nil {
			return nil, 0, 0, err
		}
		r.Mine = r.UserID == user
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
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
	if err := s.db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM place_reviews WHERE "+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT r.id,COALESCE(r.task_id,''),r.user_id,r.anonymous,r.author_label,r.rating,r.comment,r.created_at
		 FROM place_reviews r WHERE `+where+`
		 ORDER BY r.created_at DESC,r.id DESC LIMIT ? OFFSET ?`,
		queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]merchant.Review, 0)
	for rows.Next() {
		var r merchant.Review
		if err := rows.Scan(&r.ID, &r.TaskID, &r.UserID, &r.Anonymous, &r.AuthorLabel, &r.Rating, &r.Comment, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, count, nil
}

func (s *Store) RevealPlaceReviewIdentity(ctx context.Context, reviewID, adminID, reason string) (string, error) {
	tx, err := s.db.SQL().BeginTx(ctx, nil)
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
