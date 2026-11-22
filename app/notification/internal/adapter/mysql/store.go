package notificationadapter

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"cityflow/app/notification/internal/application/notificationapp"
	"cityflow/app/notification/internal/domain/notification"
	platformmysql "cityflow/internal/platform/mysql"
)

type Store struct{ db *platformmysql.DB }

func New(db *platformmysql.DB) *Store { return &Store{db: db} }

var _ notificationapp.Store = (*Store)(nil)

type notificationRecord struct {
	ID        string     `gorm:"column:id;primaryKey"`
	UserID    string     `gorm:"column:user_id"`
	Kind      string     `gorm:"column:kind"`
	Title     string     `gorm:"column:title"`
	Body      string     `gorm:"column:body"`
	TaskID    *string    `gorm:"column:task_id"`
	EventID   *string    `gorm:"column:event_id"`
	ReadAt    *time.Time `gorm:"column:read_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
}

func (notificationRecord) TableName() string { return "notifications" }

func (s *Store) List(ctx context.Context, userID string, unreadOnly bool, page notificationapp.Page) ([]*notification.Notification, int, error) {
	var records []notificationRecord
	query := s.db.ORM.WithContext(ctx).Model(&notificationRecord{}).Where("user_id = ?", userID)
	if unreadOnly {
		query = query.Where("read_at IS NULL")
	}
	if err := query.Order("created_at DESC").Order("id DESC").Limit(page.Limit).Offset(page.Offset).Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	unread, err := s.Unread(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*notification.Notification, 0, len(records))
	for _, record := range records {
		out = append(out, fromRecord(record))
	}
	return out, unread, nil
}

func (s *Store) Unread(ctx context.Context, userID string) (int, error) {
	var count int64
	if err := s.db.ORM.WithContext(ctx).Model(&notificationRecord{}).
		Where("user_id = ? AND read_at IS NULL", userID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return int(count), nil
}

func (s *Store) MarkRead(ctx context.Context, userID string, ids []string, all bool) error {
	query := s.db.ORM.WithContext(ctx).Model(&notificationRecord{}).
		Where("user_id = ? AND read_at IS NULL", userID)
	if !all {
		query = query.Where("id IN ?", ids)
	}
	if err := query.Update("read_at", time.Now().UTC()).Error; err != nil {
		return fmt.Errorf("mark notifications read: %w", err)
	}
	return nil
}

func (s *Store) Create(ctx context.Context, item *notification.Notification) error {
	if item == nil {
		return errors.New("notification is nil")
	}
	if item.ID == "" {
		item.ID = newID()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	record := notificationRecord{
		ID: item.ID, UserID: item.UserID, Kind: item.Kind, Title: item.Title, Body: item.Body,
		TaskID: optional(item.TaskID), EventID: optional(item.EventID),
		ReadAt: item.ReadAt, CreatedAt: item.CreatedAt,
	}
	result := s.db.ORM.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
	if result.Error != nil {
		return fmt.Errorf("insert notification: %w", result.Error)
	}
	return nil
}

// Purge deletes notifications past their retention window. Read items use a
// shorter window than unread items so unread alerts survive longer.
func (s *Store) Purge(ctx context.Context, readBefore, unreadBefore time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := s.db.ORM.WithContext(ctx).Exec(
		"DELETE FROM notifications WHERE (read_at IS NOT NULL AND read_at < ?) OR (read_at IS NULL AND created_at < ?) LIMIT ?",
		readBefore, unreadBefore, limit,
	)
	if result.Error != nil {
		return 0, fmt.Errorf("purge notifications: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func fromRecord(record notificationRecord) *notification.Notification {
	return &notification.Notification{
		ID: record.ID, UserID: record.UserID, Kind: record.Kind, Title: record.Title, Body: record.Body,
		TaskID: value(record.TaskID), EventID: value(record.EventID),
		ReadAt: record.ReadAt, CreatedAt: record.CreatedAt,
	}
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func newID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%032x", time.Now().UTC().UnixNano())
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16])
}
