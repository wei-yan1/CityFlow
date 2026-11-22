// Package reaction implements like/favorite writes and reads. Redis is the
// request-path source of truth; content services receive absolute like counts
// through MQ and only maintain their own read-model column.
package reaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"cityflow/internal/common/contract"
	"cityflow/internal/common/domain"
	"cityflow/internal/platform/messaging"
)

const projectionBatchSize = 30
const projectionInterval = 20 * time.Second

type State struct {
	TargetID string
	Count    int64
	Active   bool
}

type Pending struct {
	TargetID string
	Count    int64
}

type Mine struct {
	TargetID  string
	CreatedAt time.Time
}

type Store interface {
	Set(ctx context.Context, targetType, targetID, userID, kind string, active bool, now time.Time) (bool, int64, error)
	States(ctx context.Context, targetType string, ids []string, userID, kind string) ([]State, error)
	ListMine(ctx context.Context, userID, kind, targetType, cursor string, limit int) ([]Mine, string, error)
	PopPending(ctx context.Context, targetType string, limit int64) ([]Pending, error)
	RequeuePending(ctx context.Context, targetType string, items []Pending) error
	Ping(ctx context.Context) error
}

type Publisher interface {
	Publish(ctx context.Context, msg messaging.Message) error
}

type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

type SetResult struct {
	Active bool
	Count  int64
}

func (s *Service) Set(ctx context.Context, targetType, targetID, userID, kind string, active bool) (SetResult, error) {
	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	kind = strings.TrimSpace(kind)
	if !contract.ValidReactionTargetType(targetType) || !contract.ValidReactionKind(kind) {
		return SetResult{}, fmt.Errorf("%w: unsupported target_type or kind", domain.ErrInvalidRequest)
	}
	if targetID == "" || len(targetID) > 128 {
		return SetResult{}, fmt.Errorf("%w: invalid target_id", domain.ErrInvalidRequest)
	}
	if strings.TrimSpace(userID) == "" {
		return SetResult{}, domain.ErrUnauthorized
	}
	activeResult, count, err := s.store.Set(ctx, targetType, targetID, userID, kind, active, s.now())
	if err != nil {
		return SetResult{}, fmt.Errorf("%w: reaction store unavailable", domain.ErrUnavailable)
	}
	return SetResult{Active: activeResult, Count: count}, nil
}

func (s *Service) States(ctx context.Context, targetType string, ids []string, userID, kind string) ([]State, error) {
	targetType = strings.TrimSpace(targetType)
	kind = strings.TrimSpace(kind)
	if !contract.ValidReactionTargetType(targetType) || !contract.ValidReactionKind(kind) {
		return nil, fmt.Errorf("%w: unsupported target_type or kind", domain.ErrInvalidRequest)
	}
	ids = uniqueIDs(ids, 50)
	if len(ids) == 0 {
		return []State{}, nil
	}
	states, err := s.store.States(ctx, targetType, ids, userID, kind)
	if err != nil {
		return nil, fmt.Errorf("%w: reaction store unavailable", domain.ErrUnavailable)
	}
	return states, nil
}

func (s *Service) ListMine(ctx context.Context, userID, kind, targetType, cursor string, limit int) ([]Mine, string, error) {
	targetType = strings.TrimSpace(targetType)
	kind = strings.TrimSpace(kind)
	if !contract.ValidReactionTargetType(targetType) || !contract.ValidReactionKind(kind) {
		return nil, "", fmt.Errorf("%w: unsupported target_type or kind", domain.ErrInvalidRequest)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	items, next, err := s.store.ListMine(ctx, userID, kind, targetType, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("%w: reaction store unavailable", domain.ErrUnavailable)
	}
	return items, next, nil
}

// RunProjector periodically drains the absolute-count ZSETs and publishes one
// batch per target type to like.record.topic. A failed publish requeues the
// batch; duplicate batches are safe because content services assign absolute
// counts rather than incrementing.
func (s *Service) RunProjector(ctx context.Context, targetTypes []string, publisher Publisher, interval time.Duration, batchSize int64) {
	if publisher == nil {
		log.Printf("reaction projector disabled: RabbitMQ publisher is not configured")
		return
	}
	if interval <= 0 {
		interval = projectionInterval
	}
	if batchSize <= 0 {
		batchSize = projectionBatchSize
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, targetType := range targetTypes {
				s.projectOnce(ctx, targetType, publisher, batchSize)
			}
		}
	}
}

type projectionItem struct {
	BizID      string `json:"bizId"`
	LikedTimes int64  `json:"likedTimes"`
}

func (s *Service) projectOnce(ctx context.Context, targetType string, publisher Publisher, batchSize int64) {
	items, err := s.store.PopPending(ctx, targetType, batchSize)
	if err != nil {
		log.Printf("reaction projector %s: pop: %v", targetType, err)
		return
	}
	if len(items) == 0 {
		return
	}
	payloadItems := make([]projectionItem, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, projectionItem{BizID: item.TargetID, LikedTimes: item.Count})
	}
	payload, err := json.Marshal(payloadItems)
	if err != nil {
		log.Printf("reaction projector %s: encode: %v", targetType, err)
		_ = s.store.RequeuePending(ctx, targetType, items)
		return
	}
	msg := messaging.Message{
		ID:          fmt.Sprintf("like-%s-%d", targetType, time.Now().UTC().UnixNano()),
		Type:        targetType + ".times.changed",
		AggregateID: targetType,
		OccurredAt:  time.Now().UTC(),
		Payload:     payload,
	}
	if err := publisher.Publish(ctx, msg); err != nil {
		_ = s.store.RequeuePending(ctx, targetType, items)
		log.Printf("reaction projector %s: publish: %v", targetType, err)
	}
}

func (s *Service) Health(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("reaction store is not configured")
	}
	return s.store.Ping(ctx)
}

func uniqueIDs(raw []string, max int) []string {
	if max <= 0 {
		max = 50
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == max {
			break
		}
	}
	return out
}
