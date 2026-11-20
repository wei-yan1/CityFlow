package mysqlstore

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"cityflow/internal/domain"
	"cityflow/internal/domain/reaction"
)

type ReactionStore struct{ Store *Store }

func (r ReactionStore) Apply(ctx context.Context, ops []reaction.Op) error {
	if len(ops) == 0 {
		return nil
	}
	tx, err := r.Store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	inserts := make([]reaction.Op, 0, len(ops))
	deletes := make([]reaction.Op, 0, len(ops))
	for _, op := range ops {
		if op.TargetType == "" || op.TargetID == "" || op.UserID == "" || !reaction.ValidKind(op.Kind) {
			continue
		}
		if op.Active {
			inserts = append(inserts, op)
		} else {
			deletes = append(deletes, op)
		}
	}
	if len(inserts) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("(?,?,?,?,UTC_TIMESTAMP(3)),", len(inserts)), ",")
		args := make([]any, 0, len(inserts)*4)
		for _, op := range inserts {
			args = append(args, op.TargetType, op.TargetID, op.UserID, op.Kind)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT IGNORE INTO reaction_records(target_type,target_id,user_id,kind,created_at) VALUES `+placeholders,
			args...,
		); err != nil {
			return err
		}
	}
	if len(deletes) > 0 {
		// Group deletes by kind so a batch remains a single statement per kind.
		byKind := map[string][]reaction.Op{}
		for _, op := range deletes {
			byKind[op.Kind] = append(byKind[op.Kind], op)
		}
		for kind, items := range byKind {
			placeholders := strings.TrimSuffix(strings.Repeat("(?,?,?),", len(items)), ",")
			args := make([]any, 0, len(items)*3+1)
			args = append(args, kind)
			for _, op := range items {
				args = append(args, op.TargetType, op.TargetID, op.UserID)
			}
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM reaction_records WHERE kind=? AND (target_type,target_id,user_id) IN (`+placeholders+`)`,
				args...,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (r ReactionStore) Count(ctx context.Context, targetType, targetID, kind string) (int64, error) {
	var count int64
	err := r.Store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM reaction_records WHERE target_type=? AND target_id=? AND kind=?`,
		targetType, targetID, kind,
	).Scan(&count)
	return count, err
}

func (r ReactionStore) ListMine(ctx context.Context, userID, kind, targetType, cursor string, limit int) ([]reaction.Record, string, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	query := `SELECT id,target_type,target_id,user_id,kind,created_at
		FROM reaction_records WHERE user_id=? AND kind=? AND target_type=?`
	args := []any{userID, kind, targetType}
	if cursor != "" {
		at, id, err := decodeReactionCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("%w: invalid reaction cursor", domain.ErrInvalidRequest)
		}
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, at, at, id)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := r.Store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]reaction.Record, 0)
	for rows.Next() {
		var item reaction.Record
		if err := rows.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.UserID, &item.Kind, &item.CreatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeReactionCursor(last.CreatedAt, last.ID)
	}
	return out, next, nil
}

func (r ReactionStore) Rebuild(ctx context.Context, targetType string) ([]reaction.Record, error) {
	rows, err := r.Store.db.QueryContext(ctx,
		`SELECT id,target_type,target_id,user_id,kind,created_at
		 FROM reaction_records WHERE target_type=? ORDER BY id`, targetType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]reaction.Record, 0)
	for rows.Next() {
		var item reaction.Record
		if err := rows.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.UserID, &item.Kind, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r ReactionStore) Ping(ctx context.Context) error { return r.Store.Ping(ctx) }

func encodeReactionCursor(createdAt time.Time, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%d", createdAt.UTC().Format(time.RFC3339Nano), id)))
}

func decodeReactionCursor(cursor string) (time.Time, uint64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("missing cursor parts")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, err
	}
	var id uint64
	if _, err := fmt.Sscanf(parts[1], "%d", &id); err != nil {
		return time.Time{}, 0, err
	}
	return at, id, nil
}
