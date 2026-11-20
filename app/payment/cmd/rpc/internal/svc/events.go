package svc

import (
	"context"
	"log"

	"cityflow/internal/platform/messaging"
)

// StartEvents starts the Payment consumer. The RPC server and the settlement
// worker continue to run independently; the consumer only handles task facts
// that must not be lost when the Task process returns early.
func (s *ServiceContext) StartEvents(ctx context.Context) {
	if s == nil || s.Consumer == nil {
		return
	}
	go func() {
		err := s.Consumer.Consume(ctx, "", nil, func(callCtx context.Context, msg messaging.Message) error {
			return s.Payments.HandleTaskEvent(callCtx, msg.ID, msg.Type, msg.Payload)
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("payment task consumer stopped: %v", err)
		}
	}()
}
