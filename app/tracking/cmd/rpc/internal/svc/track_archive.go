package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	trackingservice "cityflow/app/tracking/application/tracking"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/messaging"
)

// StartTrackArchive starts the MQ consumer first and waits for its durable
// queue/binding to be declared before the archiver starts publishing. Without
// that ordering, an early publish could be routed to an exchange with no
// binding and be lost.
func (s *ServiceContext) StartTrackArchive(ctx context.Context) {
	if s == nil || s.Consumer == nil || s.TrackArchiver == nil {
		return
	}
	go func() {
		_ = s.Consumer.Consume(ctx, "", nil, s.handleTrackBatch)
	}()
	ready := s.Consumer.Ready()
	if ready == nil {
		return
	}
	go func() {
		select {
		case <-ready:
			s.TrackArchiver.Run(ctx)
		case <-ctx.Done():
		}
	}()
}

func (s *ServiceContext) handleTrackBatch(ctx context.Context, msg messaging.Message) error {
	if msg.Type != trackingservice.TrackArchiveTopic {
		return nil
	}
	if s.TrackArchive == nil {
		return errors.New("tracking MySQL archive is unavailable")
	}
	if s.TrackQueue == nil {
		return errors.New("tracking Redis queue is unavailable")
	}
	var payload trackingservice.TrackBatchPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return fmt.Errorf("decode track archive batch: %w", err)
	}
	if payload.RiderID == "" {
		return errors.New("track archive batch has no rider_id")
	}
	points := make([]location.TrackPoint, 0, len(payload.Points))
	members := make([]string, 0, len(payload.Points))
	for _, item := range payload.Points {
		if item.Member == "" {
			return fmt.Errorf("track archive batch for %s has an empty Redis member", payload.RiderID)
		}
		if item.Position.RiderID != "" && item.Position.RiderID != payload.RiderID {
			return fmt.Errorf("track archive batch rider mismatch: %s != %s", item.Position.RiderID, payload.RiderID)
		}
		position := item.Position
		position.RiderID = payload.RiderID
		if position.Source == "" {
			position.Source = "device"
		}
		id := uint64(position.Version)
		if position.Version <= 0 {
			id = uint64(position.ObservedAt.UnixMilli())
		}
		points = append(points, location.TrackPoint{ID: id, TaskID: position.TaskID, Position: position})
		members = append(members, item.Member)
	}
	if len(points) == 0 {
		return nil
	}
	if err := s.TrackArchive.ArchiveTrackPoints(ctx, points); err != nil {
		return fmt.Errorf("archive track batch: %w", err)
	}
	if err := s.TrackQueue.RemoveTrackMembers(ctx, payload.RiderID, members); err != nil {
		return fmt.Errorf("remove archived Redis track members: %w", err)
	}
	_, _ = s.TrackQueue.ForgetRiderIfEmpty(ctx, payload.RiderID)
	return nil
}
