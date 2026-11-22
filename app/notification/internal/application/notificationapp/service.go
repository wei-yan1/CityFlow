package notificationapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cityflow/app/notification/internal/domain/notification"
)

type Page struct {
	Limit  int
	Offset int
}

type Store interface {
	List(context.Context, string, bool, Page) ([]*notification.Notification, int, error)
	Unread(context.Context, string) (int, error)
	MarkRead(context.Context, string, []string, bool) error
	Create(context.Context, *notification.Notification) error
	Purge(context.Context, time.Time, time.Time, int) (int64, error)
}

type Service struct {
	store Store
}

func New(store Store) *Service { return &Service{store: store} }

func (s *Service) List(ctx context.Context, userID string, unreadOnly bool, page Page) ([]*notification.Notification, int, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, 0, fmt.Errorf("%w: user id is required", notification.ErrInvalidRequest)
	}
	return s.store.List(ctx, userID, unreadOnly, normalizePage(page))
}

func (s *Service) Unread(ctx context.Context, userID string) (int, error) {
	if strings.TrimSpace(userID) == "" {
		return 0, fmt.Errorf("%w: user id is required", notification.ErrInvalidRequest)
	}
	return s.store.Unread(ctx, userID)
}

func (s *Service) MarkRead(ctx context.Context, userID string, ids []string, all bool) (int, error) {
	if strings.TrimSpace(userID) == "" {
		return 0, fmt.Errorf("%w: user id is required", notification.ErrInvalidRequest)
	}
	if !all && len(ids) == 0 {
		return s.store.Unread(ctx, userID)
	}
	if err := s.store.MarkRead(ctx, userID, ids, all); err != nil {
		return 0, err
	}
	return s.store.Unread(ctx, userID)
}

// PurgeRetention expires read notifications sooner than unread ones. Product
// policy supplies both windows; the service only bounds each invocation.
func (s *Service) PurgeRetention(ctx context.Context, readRetention, unreadRetention time.Duration, batch, maxPasses int) (int64, error) {
	if readRetention <= 0 || unreadRetention <= 0 {
		return 0, fmt.Errorf("notification retention must be positive")
	}
	if batch <= 0 {
		batch = 1000
	}
	if maxPasses <= 0 {
		maxPasses = 5
	}
	now := time.Now().UTC()
	readBefore := now.Add(-readRetention)
	unreadBefore := now.Add(-unreadRetention)
	var total int64
	for pass := 0; pass < maxPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := s.store.Purge(ctx, readBefore, unreadBefore, batch)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < int64(batch) {
			break
		}
	}
	return total, nil
}

func normalizePage(page Page) Page {
	if page.Limit <= 0 {
		page.Limit = 20
	}
	if page.Limit > 100 {
		page.Limit = 100
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return page
}
