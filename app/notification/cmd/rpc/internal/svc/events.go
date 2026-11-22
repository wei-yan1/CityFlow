package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"cityflow/app/notification/internal/domain/notification"
	"cityflow/internal/platform/messaging"
)

func (s *ServiceContext) StartEvents(ctx context.Context) {
	if s == nil || s.Consumer == nil {
		return
	}
	go func() {
		if err := s.Consumer.Consume(ctx, "", nil, s.handleEvent); err != nil && ctx.Err() == nil {
			log.Printf("notification consumer stopped: %v", err)
		}
	}()
}

type eventEnvelope struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	TaskID    string `json:"task_id"`
	CreatedBy string `json:"created_by"`
	Title     string `json:"title"`
}

func (s *ServiceContext) handleEvent(ctx context.Context, msg messaging.Message) error {
	var event eventEnvelope
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		return fmt.Errorf("decode %s: %w", msg.Type, err)
	}
	userID := event.CreatedBy
	if userID == "" {
		userID = event.UserID
	}
	taskID := event.TaskID
	if taskID == "" {
		taskID = event.ID
	}
	if userID == "" || taskID == "" {
		return nil
	}
	item := &notification.Notification{
		UserID: userID, EventID: msg.ID, TaskID: taskID,
	}
	switch msg.Type {
	case "task.created.v1":
		item.Kind = "TASK_CREATED"
		item.Title = "任务已创建"
		item.Body = "任务「" + event.Title + "」已创建，请完成支付。"
	case "task.completed.v1":
		item.Kind = "TASK_COMPLETED"
		item.Title = "任务已完成"
		item.Body = "本次配送已完成，感谢使用 CityFlow。"
	case "task.cancelled.v1":
		item.Kind = "TASK_CANCELLED"
		item.Title = "任务已取消"
		item.Body = "本次任务已取消。"
	default:
		return nil
	}
	return s.Store.Create(ctx, item)
}
