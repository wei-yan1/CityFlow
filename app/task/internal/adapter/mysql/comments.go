package taskmysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"cityflow/app/task/domain/comment"
	"cityflow/app/task/repository"
	"cityflow/internal/common/domain"
)

const riderCommentTable = "rider_feedback_comments"

// RiderComments implements the review-comment contract against
// rider_feedback_comments. Rider comments never enter this adapter.
type RiderComments struct{ Store *Store }

var _ repository.ReviewComments = RiderComments{}

func (p RiderComments) ReviewExists(ctx context.Context, reviewID string) (bool, error) {
	var exists bool
	err := p.Store.db.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rider_feedback WHERE id=? AND kind='REVIEW')`, reviewID).Scan(&exists)
	return exists, err
}

func (p RiderComments) CreateComment(ctx context.Context, item comment.Record) error {
	var parentID, replyUser, replyLabel any
	if item.ParentID != "" {
		parentID = item.ParentID
	}
	if item.ReplyToUserID != "" {
		replyUser = item.ReplyToUserID
	}
	if item.ReplyToLabel != "" {
		replyLabel = item.ReplyToLabel
	}
	_, err := p.Store.db.SQL().ExecContext(ctx, `INSERT INTO `+riderCommentTable+`
		(id,feedback_id,user_id,anonymous,author_label,reply_to_user_id,reply_to_label,root_id,parent_id,depth,content,hidden,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.ReviewID, item.UserID, item.Anonymous, item.AuthorLabel,
		replyUser, replyLabel, item.RootID, parentID, item.Depth, item.Content, item.Hidden, item.CreatedAt)
	if err != nil {
		return writeError(err)
	}
	return nil
}

func (p RiderComments) GetComment(ctx context.Context, reviewID, id string) (comment.Record, error) {
	row := p.Store.db.SQL().QueryRowContext(ctx, `SELECT
		id,feedback_id,user_id,anonymous,author_label,COALESCE(reply_to_user_id,''),COALESCE(reply_to_label,''),
		root_id,COALESCE(parent_id,''),depth,content,hidden,like_count,created_at
		FROM `+riderCommentTable+` WHERE feedback_id=? AND id=?`, reviewID, id)
	var item comment.Record
	if err := scanComment(row, &item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return comment.Record{}, domain.ErrNotFound
		}
		return comment.Record{}, err
	}
	return item, nil
}

func (p RiderComments) ListTopComments(ctx context.Context, reviewID, sortBy, cursor string, limit int) ([]comment.Record, string, error) {
	if limit <= 0 || limit > comment.MaxPage {
		limit = 20
	}
	if sortBy != comment.SortHot {
		sortBy = comment.SortLatest
	}
	query := `SELECT
		id,feedback_id,user_id,anonymous,author_label,COALESCE(reply_to_user_id,''),COALESCE(reply_to_label,''),
		root_id,COALESCE(parent_id,''),depth,content,hidden,like_count,created_at
		FROM ` + riderCommentTable + ` WHERE feedback_id=? AND parent_id IS NULL AND hidden=0`
	args := []any{reviewID}
	if strings.TrimSpace(cursor) != "" {
		if sortBy == comment.SortHot {
			likeCount, at, id, err := comment.DecodeHotCursor(cursor)
			if err != nil {
				return nil, "", err
			}
			query += ` AND (like_count < ? OR (like_count = ? AND (created_at < ? OR (created_at = ? AND id < ?))))`
			args = append(args, likeCount, likeCount, at, at, id)
		} else {
			at, id, err := comment.DecodeCursor(cursor)
			if err != nil {
				return nil, "", err
			}
			query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
			args = append(args, at, at, id)
		}
	}
	if sortBy == comment.SortHot {
		query += ` ORDER BY like_count DESC, created_at DESC, id DESC LIMIT ?`
	} else {
		query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	}
	args = append(args, limit+1)
	rows, err := p.Store.db.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query top comments: %w", err)
	}
	defer rows.Close()
	items, err := scanComments(rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		if sortBy == comment.SortHot {
			next = comment.EncodeHotCursor(last.LikeCount, last.CreatedAt, last.ID)
		} else {
			next = comment.EncodeCursor(last.CreatedAt, last.ID)
		}
	}
	if len(items) == 0 {
		return items, next, nil
	}
	rootIDs := make([]any, 0, len(items))
	for i := range items {
		rootIDs = append(rootIDs, items[i].ID)
	}
	countQuery := `SELECT root_id, COUNT(*) FROM ` + riderCommentTable + ` WHERE feedback_id=? AND root_id IN (` + placeholders(len(rootIDs)) + `) AND hidden=0 GROUP BY root_id`
	countArgs := append([]any{reviewID}, rootIDs...)
	countRows, err := p.Store.db.SQL().QueryContext(ctx, countQuery, countArgs...)
	if err != nil {
		return nil, "", fmt.Errorf("count comment replies: %w", err)
	}
	defer countRows.Close()
	for countRows.Next() {
		var rootID string
		var count int
		if err := countRows.Scan(&rootID, &count); err != nil {
			return nil, "", err
		}
		for i := range items {
			if items[i].ID == rootID && count > 0 {
				items[i].ReplyCount = count - 1
			}
		}
	}
	if err := countRows.Err(); err != nil {
		return nil, "", err
	}
	return items, next, nil
}

func (p RiderComments) ListCommentThread(ctx context.Context, reviewID, rootID string, limit int) ([]comment.Record, error) {
	if limit <= 0 || limit > comment.ThreadPage {
		limit = comment.ThreadPage
	}
	rows, err := p.Store.db.SQL().QueryContext(ctx, `SELECT
		id,feedback_id,user_id,anonymous,author_label,COALESCE(reply_to_user_id,''),COALESCE(reply_to_label,''),
		root_id,COALESCE(parent_id,''),depth,content,hidden,like_count,created_at
		FROM `+riderCommentTable+` WHERE feedback_id=? AND root_id=? AND hidden=0
		ORDER BY created_at ASC, id ASC LIMIT ?`, reviewID, rootID, limit)
	if err != nil {
		return nil, fmt.Errorf("query comment thread: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

func (p RiderComments) HideComment(ctx context.Context, reviewID, id string) error {
	tx, err := p.Store.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM `+riderCommentTable+` WHERE feedback_id=? AND id=?)`,
		reviewID, id,
	).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+riderCommentTable+` SET hidden=1 WHERE feedback_id=? AND id=?`, reviewID, id); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM `+riderCommentTable+` WHERE feedback_id=? AND parent_id=?`, reviewID, id)
	if err != nil {
		return err
	}
	childIDs := make([]string, 0, 32)
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			_ = rows.Close()
			return err
		}
		childIDs = append(childIDs, childID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(childIDs) == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+riderCommentTable+` SET hidden=1 WHERE feedback_id=? AND parent_id=?`, reviewID, id); err != nil {
		return err
	}
	args := []any{reviewID}
	for _, childID := range childIDs {
		args = append(args, childID)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+riderCommentTable+` SET hidden=1 WHERE feedback_id=? AND parent_id IN (`+placeholders(len(childIDs))+`)`, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func scanComment(row interface{ Scan(...any) error }, item *comment.Record) error {
	var anonymous, hidden int
	if err := row.Scan(&item.ID, &item.ReviewID, &item.UserID, &anonymous, &item.AuthorLabel,
		&item.ReplyToUserID, &item.ReplyToLabel, &item.RootID, &item.ParentID,
		&item.Depth, &item.Content, &hidden, &item.LikeCount, &item.CreatedAt); err != nil {
		return err
	}
	item.Anonymous = anonymous != 0
	item.Hidden = hidden != 0
	return nil
}

func scanComments(rows *sql.Rows) ([]comment.Record, error) {
	items := make([]comment.Record, 0)
	for rows.Next() {
		var item comment.Record
		if err := scanComment(rows, &item); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comments: %w", err)
	}
	return items, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
