package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/comment"
)

// PlaceComments owns review comments in the Discovery database.
type PlaceComments struct{ Store *Store }

// RiderComments owns rider-feedback comments in the Task database.
type RiderComments struct{ Store *Store }

var _ repository.ReviewComments = PlaceComments{}
var _ repository.ReviewComments = RiderComments{}

const (
	placeCommentTable = "place_review_comments"
	riderCommentTable = "rider_feedback_comments"
)

func (p PlaceComments) ReviewExists(ctx context.Context, reviewID string) (bool, error) {
	return p.Store.targetExists(ctx, `SELECT EXISTS(SELECT 1 FROM place_reviews WHERE id=?)`, reviewID)
}

func (p PlaceComments) CreateComment(ctx context.Context, item comment.Record) error {
	return p.Store.createComment(ctx, placeCommentTable, item)
}

func (p PlaceComments) GetComment(ctx context.Context, reviewID, id string) (comment.Record, error) {
	return p.Store.getComment(ctx, placeCommentTable, reviewID, id)
}

func (p PlaceComments) ListTopComments(ctx context.Context, reviewID, cursor string, limit int) ([]comment.Record, string, error) {
	return p.Store.listTopComments(ctx, placeCommentTable, reviewID, cursor, limit)
}

func (p PlaceComments) ListCommentThread(ctx context.Context, reviewID, rootID string, limit int) ([]comment.Record, error) {
	return p.Store.listCommentThread(ctx, placeCommentTable, reviewID, rootID, limit)
}

func (p PlaceComments) HideComment(ctx context.Context, reviewID, id string) error {
	return p.Store.hideComment(ctx, placeCommentTable, reviewID, id)
}

func (r RiderComments) ReviewExists(ctx context.Context, feedbackID string) (bool, error) {
	return r.Store.targetExists(ctx, `SELECT EXISTS(SELECT 1 FROM rider_feedback WHERE id=? AND kind='REVIEW')`, feedbackID)
}

func (r RiderComments) CreateComment(ctx context.Context, item comment.Record) error {
	return r.Store.createComment(ctx, riderCommentTable, item)
}

func (r RiderComments) GetComment(ctx context.Context, feedbackID, id string) (comment.Record, error) {
	return r.Store.getComment(ctx, riderCommentTable, feedbackID, id)
}

func (r RiderComments) ListTopComments(ctx context.Context, feedbackID, cursor string, limit int) ([]comment.Record, string, error) {
	return r.Store.listTopComments(ctx, riderCommentTable, feedbackID, cursor, limit)
}

func (r RiderComments) ListCommentThread(ctx context.Context, feedbackID, rootID string, limit int) ([]comment.Record, error) {
	return r.Store.listCommentThread(ctx, riderCommentTable, feedbackID, rootID, limit)
}

func (r RiderComments) HideComment(ctx context.Context, feedbackID, id string) error {
	return r.Store.hideComment(ctx, riderCommentTable, feedbackID, id)
}

func (s *Store) targetExists(ctx context.Context, query, id string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, query, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Store) createComment(ctx context.Context, table string, item comment.Record) error {
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO `+table+`
		(id,`+commentTargetColumn(table)+`,user_id,anonymous,author_label,reply_to_user_id,reply_to_label,root_id,parent_id,depth,content,hidden,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.ReviewID, item.UserID, item.Anonymous, item.AuthorLabel,
		replyUser, replyLabel, item.RootID, parentID, item.Depth, item.Content, item.Hidden, item.CreatedAt)
	if err != nil {
		return writeError(err)
	}
	return nil
}

func (s *Store) getComment(ctx context.Context, table, reviewID, id string) (comment.Record, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id,`+commentTargetColumn(table)+`,user_id,anonymous,author_label,COALESCE(reply_to_user_id,''),COALESCE(reply_to_label,''),
		root_id,COALESCE(parent_id,''),depth,content,hidden,created_at
		FROM `+table+` WHERE `+commentTargetColumn(table)+`=? AND id=?`, reviewID, id)
	var item comment.Record
	if err := scanComment(row, &item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return comment.Record{}, domain.ErrNotFound
		}
		return comment.Record{}, err
	}
	return item, nil
}

func (s *Store) listTopComments(ctx context.Context, table, reviewID, cursor string, limit int) ([]comment.Record, string, error) {
	if limit <= 0 || limit > comment.MaxPage {
		limit = 20
	}
	query := `SELECT
		id,` + commentTargetColumn(table) + `,user_id,anonymous,author_label,COALESCE(reply_to_user_id,''),COALESCE(reply_to_label,''),
		root_id,COALESCE(parent_id,''),depth,content,hidden,created_at
		FROM ` + table + ` WHERE ` + commentTargetColumn(table) + `=? AND parent_id IS NULL AND hidden=0`
	args := []any{reviewID}
	if strings.TrimSpace(cursor) != "" {
		at, id, err := comment.DecodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, at, at, id)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
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
		next = comment.EncodeCursor(last.CreatedAt, last.ID)
	}
	if len(items) == 0 {
		return items, next, nil
	}
	rootIDs := make([]any, 0, len(items))
	for i := range items {
		rootIDs = append(rootIDs, items[i].ID)
	}
	countQuery := `SELECT root_id, COUNT(*) FROM ` + table + ` WHERE ` + commentTargetColumn(table) + `=? AND root_id IN (` + placeholders(len(rootIDs)) + `) AND hidden=0 GROUP BY root_id`
	countArgs := append([]any{reviewID}, rootIDs...)
	countRows, err := s.db.QueryContext(ctx, countQuery, countArgs...)
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

func (s *Store) listCommentThread(ctx context.Context, table, reviewID, rootID string, limit int) ([]comment.Record, error) {
	if limit <= 0 || limit > comment.ThreadPage {
		limit = comment.ThreadPage
	}
	rows, err := s.db.QueryContext(ctx, `SELECT
		id,`+commentTargetColumn(table)+`,user_id,anonymous,author_label,COALESCE(reply_to_user_id,''),COALESCE(reply_to_label,''),
		root_id,COALESCE(parent_id,''),depth,content,hidden,created_at
		FROM `+table+` WHERE `+commentTargetColumn(table)+`=? AND root_id=? AND hidden=0
		ORDER BY created_at ASC, id ASC LIMIT ?`, reviewID, rootID, limit)
	if err != nil {
		return nil, fmt.Errorf("query comment thread: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

func (s *Store) hideComment(ctx context.Context, table, reviewID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM `+table+` WHERE `+commentTargetColumn(table)+`=? AND id=?)`,
		reviewID, id,
	).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.ErrNotFound
	}
	// Hide the target itself and its first two descendant levels. Re-hiding an
	// already hidden comment is idempotent.
	if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET hidden=1 WHERE `+commentTargetColumn(table)+`=? AND id=?`, reviewID, id); err != nil {
		return err
	}

	// Second level.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM `+table+` WHERE `+commentTargetColumn(table)+`=? AND parent_id=?`, reviewID, id)
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
	if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET hidden=1 WHERE `+commentTargetColumn(table)+`=? AND parent_id=?`, reviewID, id); err != nil {
		return err
	}

	// Third level. Max depth is 3, so this is the last update.
	args := []any{reviewID}
	for _, childID := range childIDs {
		args = append(args, childID)
	}
	query := `UPDATE ` + table + ` SET hidden=1 WHERE ` + commentTargetColumn(table) + `=? AND parent_id IN (` + placeholders(len(childIDs)) + `)`
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func scanComment(row interface{ Scan(...any) error }, item *comment.Record) error {
	var anonymous, hidden int
	if err := row.Scan(&item.ID, &item.ReviewID, &item.UserID, &anonymous, &item.AuthorLabel,
		&item.ReplyToUserID, &item.ReplyToLabel, &item.RootID, &item.ParentID,
		&item.Depth, &item.Content, &hidden, &item.CreatedAt); err != nil {
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

func commentTargetColumn(table string) string {
	if table == riderCommentTable {
		return "feedback_id"
	}
	return "review_id"
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
