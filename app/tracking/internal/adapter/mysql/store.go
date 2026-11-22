package trackingmysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"cityflow/internal/common/location"
	platformmysql "cityflow/internal/platform/mysql"
)

// Store owns the durable rider_track_points archive. Redis remains the live
// snapshot and pending queue; this table is the permanent trajectory source.
type Store struct {
	db *platformmysql.DB
}

func New(db *platformmysql.DB) *Store { return &Store{db: db} }

func (s *Store) ArchiveTrackPoints(ctx context.Context, points []location.TrackPoint) error {
	if s == nil || s.db == nil {
		return errors.New("tracking MySQL store is unavailable")
	}
	if len(points) == 0 {
		return nil
	}
	values := make([]string, 0, len(points))
	args := make([]any, 0, len(points)*9)
	for _, point := range points {
		if strings.TrimSpace(point.RiderID) == "" {
			return errors.New("rider_id is required for archived track point")
		}
		observedAt := point.ObservedAt
		if observedAt.IsZero() {
			observedAt = point.UpdatedAt
		}
		receivedAt := point.UpdatedAt
		if receivedAt.IsZero() {
			receivedAt = time.Now().UTC()
		}
		version := point.Version
		if version <= 0 {
			version = observedAt.UnixMilli()
		}
		if version <= 0 {
			version = receivedAt.UnixMilli()
		}
		source := point.Source
		if source == "" {
			source = "device"
		}
		var taskID any
		if point.TaskID != "" {
			taskID = point.TaskID
		}
		values = append(values, "(?,?,?,?,?,?,?,?,?)")
		args = append(args, point.RiderID, taskID, point.Latitude, point.Longitude, point.Accuracy, source, observedAt.UTC(), receivedAt.UTC(), version)
	}
	query := `INSERT INTO rider_track_points
		(rider_id, task_id, latitude, longitude, accuracy, source, observed_at, updated_at, version)
		VALUES ` + strings.Join(values, ",") + `
		ON DUPLICATE KEY UPDATE rider_id=VALUES(rider_id)`
	if _, err := s.db.SQL().ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("archive rider track batch: %w", err)
	}
	return nil
}

func (s *Store) RiderTrack(ctx context.Context, riderID string, from, to time.Time, after uint64, limit int) ([]location.TrackPoint, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("tracking MySQL store is unavailable")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 501 {
		limit = 501
	}
	query := `SELECT rider_id, task_id, latitude, longitude, accuracy, source, observed_at, updated_at, version
		FROM rider_track_points WHERE rider_id = ?`
	args := []any{riderID}
	if !from.IsZero() {
		query += " AND observed_at >= ?"
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		query += " AND observed_at <= ?"
		args = append(args, to.UTC())
	}
	if after > 0 {
		query += " AND version > ?"
		args = append(args, after)
	}
	query += " ORDER BY observed_at ASC, version ASC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read rider track archive: %w", err)
	}
	defer rows.Close()
	result := make([]location.TrackPoint, 0, limit)
	for rows.Next() {
		var (
			riderID, source string
			taskID          sql.NullString
			latitude        float64
			longitude       float64
			accuracy        float64
			observedAt      time.Time
			receivedAt      time.Time
			version         int64
		)
		if err := rows.Scan(&riderID, &taskID, &latitude, &longitude, &accuracy, &source, &observedAt, &receivedAt, &version); err != nil {
			return nil, fmt.Errorf("scan rider track archive: %w", err)
		}
		result = append(result, location.TrackPoint{
			ID:     uint64(version),
			TaskID: taskID.String,
			Position: location.Position{
				Source: source, RiderID: riderID, TaskID: taskID.String,
				Point:    location.Point{Latitude: latitude, Longitude: longitude},
				Accuracy: accuracy, ObservedAt: observedAt.UTC(), UpdatedAt: receivedAt.UTC(), Version: version,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rider track archive: %w", err)
	}
	return result, nil
}
