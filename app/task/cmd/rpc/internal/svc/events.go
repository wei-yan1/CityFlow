package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"cityflow/app/task/domain/rider"
	"cityflow/internal/common/domain"
	"cityflow/internal/platform/messaging"
)

// StartEvents starts the Task outbox relay and the rider-approval consumer. The
// two run independently: a broker outage delays publishing, while the consumer
// reconnects on its own.
func (s *ServiceContext) StartEvents(ctx context.Context) {
	if s == nil {
		return
	}
	if s.Relay != nil {
		go s.Relay.Run(ctx)
	}
	if s.Consumer != nil {
		go func() {
			if err := s.Consumer.Consume(ctx, "", nil, s.handleRiderApproved); err != nil && ctx.Err() == nil {
				log.Printf("task rider consumer stopped: %v", err)
			}
		}()
	}
	if s.LikeConsumer != nil {
		go func() {
			if err := s.LikeConsumer.Consume(ctx, "", nil, s.handleLikeCounts); err != nil && ctx.Err() == nil {
				log.Printf("task like count consumer stopped: %v", err)
			}
		}()
	}
}

type likeCountItem struct {
	BizID      string `json:"bizId"`
	LikedTimes int64  `json:"likedTimes"`
}

func (s *ServiceContext) handleLikeCounts(ctx context.Context, msg messaging.Message) error {
	targetType := ""
	switch msg.Type {
	case "RIDER_REVIEW.times.changed":
		targetType = "RIDER_REVIEW"
	case "RIDER_COMMENT.times.changed":
		targetType = "RIDER_COMMENT"
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
		if item.BizID != "" {
			counts[item.BizID] = item.LikedTimes
		}
	}
	if s.LikeCounts == nil {
		return errors.New("task like count projection store is unavailable")
	}
	if err := s.LikeCounts.UpdateLikeCounts(ctx, targetType, counts); err != nil {
		return fmt.Errorf("update %s like_count: %w", targetType, err)
	}
	return nil
}

// handleRiderApproved projects the Identity-owned approval into Task's rider
// performance table. The event is the handoff; no HTTP/RPC request is held open
// while the profile is created.
type riderApprovedEvent struct {
	UserID             string   `json:"user_id"`
	Status             string   `json:"status"`
	DisplayName        string   `json:"display_name"`
	VehicleType        string   `json:"vehicle_type"`
	Skills             []string `json:"skills"`
	ServiceArea        string   `json:"service_area"`
	MaxDistanceMeters  int      `json:"max_distance_meters"`
	MaxConcurrentTasks int      `json:"max_concurrent_tasks"`
}

func (s *ServiceContext) handleRiderApproved(ctx context.Context, msg messaging.Message) error {
	if msg.Type != "rider.approved.v1" {
		return nil
	}
	if s.RiderRepo == nil {
		return errors.New("task rider repository is unavailable")
	}
	var registration riderApprovedEvent
	if err := json.Unmarshal(msg.Payload, &registration); err != nil {
		return fmt.Errorf("decode rider approval: %w", err)
	}
	if registration.UserID == "" || registration.Status != "APPROVED" {
		return nil
	}
	request := rider.UpdateRequest{
		DisplayName: registration.DisplayName, VehicleType: registration.VehicleType,
		Skills: registration.Skills, ServiceArea: registration.ServiceArea,
		MaxDistanceMeters: registration.MaxDistanceMeters, MaxConcurrentTasks: registration.MaxConcurrentTasks,
	}
	_, err := s.RiderRepo.CreateRider(ctx, registration.UserID, request)
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrConflict) {
		// At-least-once delivery can replay the event after the profile exists.
		if _, getErr := s.RiderRepo.RiderForUser(ctx, registration.UserID); getErr == nil {
			return nil
		}
	}
	return fmt.Errorf("provision rider %s: %w", registration.UserID, err)
}
