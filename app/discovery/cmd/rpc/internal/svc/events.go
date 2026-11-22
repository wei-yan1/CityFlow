package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"cityflow/internal/platform/messaging"
)

// StartEvents starts the like-count projection consumer. Reaction publishes
// absolute counts; Discovery only assigns them to its review/comment read
// model, so a retry or duplicate batch is idempotent.
func (s *ServiceContext) StartEvents(ctx context.Context) {
	if s == nil || s.Consumer == nil {
		return
	}
	go func() {
		if err := s.Consumer.Consume(ctx, "", nil, s.handleLikeProjection); err != nil && ctx.Err() == nil {
			log.Printf("discovery like count consumer stopped: %v", err)
		}
	}()
}

type likeCountItem struct {
	BizID      string `json:"bizId"`
	LikedTimes int64  `json:"likedTimes"`
}

func (s *ServiceContext) handleLikeProjection(ctx context.Context, msg messaging.Message) error {
	targetType := ""
	switch msg.Type {
	case "PLACE_REVIEW.times.changed":
		targetType = "PLACE_REVIEW"
	case "PLACE_COMMENT.times.changed":
		targetType = "PLACE_COMMENT"
	default:
		return nil
	}
	var items []likeCountItem
	if err := json.Unmarshal(msg.Payload, &items); err != nil {
		return fmt.Errorf("decode like count batch: %w", err)
	}
	if len(items) == 0 {
		return nil
	}
	counts := make(map[string]int64, len(items))
	for _, item := range items {
		if item.BizID == "" {
			continue
		}
		counts[item.BizID] = item.LikedTimes
	}
	if s.Likes == nil {
		return fmt.Errorf("like count projection store is unavailable")
	}
	if err := s.Likes.UpdateLikeCounts(ctx, targetType, counts); err != nil {
		return fmt.Errorf("update %s like_count: %w", targetType, err)
	}
	return nil
}
