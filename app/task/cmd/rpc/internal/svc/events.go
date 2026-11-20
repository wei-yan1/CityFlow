package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"cityflow/internal/domain"
	"cityflow/internal/domain/rider"
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
}

// handleRiderApproved projects the Identity-owned approval into Task's rider
// performance table. The event is the handoff; no HTTP/RPC request is held open
// while the profile is created.
func (s *ServiceContext) handleRiderApproved(ctx context.Context, msg messaging.Message) error {
	if msg.Type != "rider.approved.v1" {
		return nil
	}
	if s.RiderRepo == nil {
		return errors.New("task rider repository is unavailable")
	}
	var registration rider.Registration
	if err := json.Unmarshal(msg.Payload, &registration); err != nil {
		return fmt.Errorf("decode rider approval: %w", err)
	}
	if registration.UserID == "" || registration.Status != rider.RegistrationApproved {
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
