package mysqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/feedback"
)

func (s *Store) CreateRiderFeedback(ctx context.Context, record feedback.Record) error {
	images, err := json.Marshal(record.Images)
	if err != nil {
		return fmt.Errorf("encode feedback images: %w", err)
	}
	var rating any
	if record.Rating > 0 {
		rating = record.Rating
	}
	var amount any
	if record.AmountFen > 0 {
		amount = record.AmountFen
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO rider_feedback
		(id,rider_id,task_id,user_id,kind,rating,amount_fen,comment,reason,details,images,status,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		record.ID, record.RiderID, record.TaskID, record.UserID, record.Kind,
		rating, amount, nullString(record.Comment), nullString(record.Reason), nullString(record.Details),
		string(images), record.Status, record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return writeError(err)
	}
	return nil
}

func (s *Store) ListRiderFeedback(ctx context.Context, riderID string, kinds []string, limit, offset int) ([]feedback.Record, error) {
	query := `SELECT id,rider_id,task_id,user_id,kind,rating,amount_fen,comment,reason,details,images,status,resolution_note,resolved_by,created_at,updated_at,resolved_at
		FROM rider_feedback WHERE rider_id = ?`
	args := []any{riderID}
	if len(kinds) > 0 {
		placeholders := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			placeholders = append(placeholders, "?")
			args = append(args, kind)
		}
		query += " AND kind IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, positiveLimit(limit), positiveOffset(offset))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query rider feedback: %w", err)
	}
	defer rows.Close()
	return scanRiderFeedback(rows)
}

func (s *Store) ListRiderReports(ctx context.Context, status string, page repository.Page) ([]feedback.Record, error) {
	query := `SELECT id,rider_id,task_id,user_id,kind,rating,amount_fen,comment,reason,details,images,status,resolution_note,resolved_by,created_at,updated_at,resolved_at
		FROM rider_feedback WHERE kind = 'REPORT'`
	args := []any{}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, positiveLimit(page.Limit), positiveOffset(page.Offset))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query rider reports: %w", err)
	}
	defer rows.Close()
	return scanRiderFeedback(rows)
}

func (s *Store) ResolveRiderReport(ctx context.Context, id, adminID, status, note string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE rider_feedback SET status = ?, resolution_note = ?, resolved_by = ?, resolved_at = UTC_TIMESTAMP(3), updated_at = UTC_TIMESTAMP(3)
		WHERE id = ? AND kind = 'REPORT'`, status, nullString(note), nullString(adminID), id)
	if err != nil {
		return writeError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve report rows: %w", err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanRiderFeedback(rows *sql.Rows) ([]feedback.Record, error) {
	items := make([]feedback.Record, 0)
	for rows.Next() {
		var item feedback.Record
		var rating, amount sql.NullInt64
		var comment, reason, details, resolutionNote, resolvedBy sql.NullString
		var images []byte
		var resolvedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.RiderID, &item.TaskID, &item.UserID, &item.Kind, &rating, &amount, &comment, &reason, &details, &images, &item.Status, &resolutionNote, &resolvedBy, &item.CreatedAt, &item.UpdatedAt, &resolvedAt); err != nil {
			return nil, fmt.Errorf("scan rider feedback: %w", err)
		}
		item.Rating = int(rating.Int64)
		item.AmountFen = amount.Int64
		item.Comment, item.Reason, item.Details = comment.String, reason.String, details.String
		item.ResolutionNote, item.ResolvedBy = resolutionNote.String, resolvedBy.String
		if len(images) > 0 {
			if err := json.Unmarshal(images, &item.Images); err != nil {
				return nil, fmt.Errorf("decode feedback images: %w", err)
			}
		}
		if resolvedAt.Valid {
			item.ResolvedAt = &resolvedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rider feedback: %w", err)
	}
	return items, nil
}

func positiveLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

func positiveOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
