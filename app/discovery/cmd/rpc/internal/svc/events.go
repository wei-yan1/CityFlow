package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"cityflow/internal/platform/messaging"
)

// StartEvents starts the projection consumer. Like writes never block on
// Discovery; Reaction publishes absolute counts and this consumer updates the
// read model used by ORDER BY and by the fallback display path.
func (s *ServiceContext) StartEvents(ctx context.Context) {
	if s == nil || s.Consumer == nil {
		return
	}
	go func() {
		if err := s.Consumer.Consume(ctx, "", nil, s.handleLikeProjection); err != nil && ctx.Err() == nil {
			log.Printf("discovery like projection consumer stopped: %v", err)
		}
	}()
}

func (s *ServiceContext) handleLikeProjection(ctx context.Context, msg messaging.Message) error {
	if msg.Type != "like.times.PLACE_REVIEW" {
		return nil
	}
	var payload struct {
		Type  string `json:"type"`
		Items []struct {
			TargetID string `json:"target_id"`
			Count    int64  `json:"count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("decode like projection: %w", err)
	}
	if s.Likes == nil {
		return fmt.Errorf("like projection store is unavailable")
	}
	for _, item := range payload.Items {
		if item.TargetID == "" {
			continue
		}
		if err := s.Likes.UpdatePlaceReviewLikeCount(ctx, item.TargetID, item.Count); err != nil {
			return fmt.Errorf("update review %s like_count: %w", item.TargetID, err)
		}
	}
	return nil
}
