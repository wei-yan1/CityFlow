package taskmysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/app/task/domain/task"
	"cityflow/app/task/repository"
	"cityflow/internal/common/domain"
)

func (s *Store) FindIdempotent(ctx context.Context, owner, key, hash string) (*task.Task, error) {
	var record taskRecord
	result := s.db.ORM.WithContext(ctx).Where("created_by = ? AND idempotency_key = ?", owner, key).Limit(1).Find(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	if stringValue(record.IdempotencyHash) != hash {
		return nil, fmt.Errorf("%w: idempotency key reused with different task", domain.ErrConflict)
	}
	return s.Get(ctx, record.ID)
}

func (s *Store) Create(ctx context.Context, req task.CreateRequest, createdBy string) (*task.Task, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		var err error
		id, err = newUUID()
		if err != nil {
			return nil, err
		}
	}
	status := strings.TrimSpace(req.InitialStatus)
	if status == "" {
		status = task.StatusOpen
	}
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record := taskRecord{
			QuotedAmountFen:  req.QuotedAmountFen,
			DestinationPOIID: req.DestinationPOIID,
			IdempotencyKey:   optionalString(req.IdempotencyKey), IdempotencyHash: optionalString(req.IdempotencyHash),
			PickupStops: req.PickupStops, DeliveryAt: req.DeliveryAt,
			PickupLocation: req.PickupLocation, DestinationLocation: req.DestinationLocation,
			ID: id, Title: strings.TrimSpace(req.Title), Description: optionalString(strings.TrimSpace(req.Description)),
			ContactName: strings.TrimSpace(req.ContactName), ContactPhone: strings.TrimSpace(req.ContactPhone), HandoverNote: optionalString(strings.TrimSpace(req.HandoverNote)),
			Type: strings.TrimSpace(req.Type), Priority: req.Priority, Pickup: strings.TrimSpace(req.Pickup),
			Destination: strings.TrimSpace(req.Destination), Status: status, CreatedBy: createdBy,
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("insert task: %w", err)
		}
		initialEvent := taskEventRecord{TaskID: id, To: task.StatusOpen, ActorID: optionalString(createdBy)}
		if err := tx.Create(&initialEvent).Error; err != nil {
			return fmt.Errorf("insert initial task event: %w", err)
		}
		payload, err := json.Marshal(toTask(record))
		if err != nil {
			return err
		}
		eventID, err := newUUID()
		if err != nil {
			return err
		}
		outbox := outboxRecord{
			ID: eventID, EventType: "task.created.v1", AggregateID: id,
			Payload: string(payload), Status: "PENDING", AvailableAt: time.Now().UTC(),
			CreatedAt: time.Now().UTC(),
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return fmt.Errorf("insert task created event: %w", err)
		}
		return nil
	})
	if err != nil {
		if req.IdempotencyKey != "" && errors.Is(err, gorm.ErrDuplicatedKey) {
			var existing taskRecord
			lookup := s.db.ORM.WithContext(ctx).Where("created_by = ? AND idempotency_key = ?", createdBy, req.IdempotencyKey).Take(&existing).Error
			if lookup != nil {
				return nil, lookup
			}
			if stringValue(existing.IdempotencyHash) != req.IdempotencyHash {
				return nil, fmt.Errorf("%w: idempotency key reused with different task", domain.ErrConflict)
			}
			return s.Get(ctx, existing.ID)
		}
		return nil, fmt.Errorf("create task: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Store) List(ctx context.Context, status string, page repository.Page) ([]*task.Task, error) {
	return s.listTasks(ctx, status, "", "", page)
}

func (s *Store) ListByCreator(ctx context.Context, creatorID, status string, page repository.Page) ([]*task.Task, error) {
	return s.listTasks(ctx, status, "created_by", creatorID, page)
}

func (s *Store) ListForRider(ctx context.Context, riderID, status string, page repository.Page) ([]*task.Task, error) {
	return s.listTasks(ctx, status, "rider", riderID, page)
}

func (s *Store) listTasks(ctx context.Context, status, scopeColumn, scopeID string, page repository.Page) ([]*task.Task, error) {
	query := s.db.ORM.WithContext(ctx).Model(&taskRecord{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	switch scopeColumn {
	case "created_by":
		query = query.Where("created_by = ?", scopeID)
	case "rider":
		query = query.Where("(status = ? OR worker_id = ?)", task.StatusOpen, scopeID)
	}
	var records []taskRecord
	if err := query.Order("created_at DESC").Order("id DESC").Scopes(paginate(page)).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	result := make([]*task.Task, 0, len(records))
	for _, record := range records {
		result = append(result, toTask(record))
	}
	if err := s.loadEventsForTasks(ctx, result); err != nil {
		return nil, err
	}
	if err := s.loadIncidentsForTasks(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) Get(ctx context.Context, id string) (*task.Task, error) {
	var record taskRecord
	err := s.db.ORM.WithContext(ctx).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	item := toTask(record)
	if err := s.loadEvents(ctx, item); err != nil {
		return nil, err
	}
	if err := s.loadIncidents(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) Update(ctx context.Context, id string, request task.UpdateRequest) (*task.Task, error) {
	pickup, err := json.Marshal(request.PickupLocation)
	if err != nil {
		return nil, err
	}
	destination, err := json.Marshal(request.DestinationLocation)
	if err != nil {
		return nil, err
	}
	stops, err := json.Marshal(request.PickupStops)
	if err != nil {
		return nil, err
	}
	result := s.db.ORM.WithContext(ctx).Model(&taskRecord{}).Where("id = ? AND status = ?", id, task.StatusOpen).Updates(map[string]any{
		"quoted_amount_fen":  request.QuotedAmountFen,
		"destination_poi_id": request.DestinationPOIID,
		"pickup_stops":       string(stops), "delivery_at": request.DeliveryAt,
		"pickup_location": string(pickup), "destination_location": string(destination),
		"title": request.Title, "description": optionalString(request.Description), "task_type": request.Type,
		"contact_name": request.ContactName, "contact_phone": request.ContactPhone, "handover_note": optionalString(request.HandoverNote),
		"priority": request.Priority, "pickup": request.Pickup, "destination": request.Destination,
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		item, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if item.Status == task.StatusOpen {
			return item, nil
		}
		return nil, domain.ErrConflict
	}
	return s.Get(ctx, id)
}

func (s *Store) Claim(ctx context.Context, id, workerID string) (*task.Task, error) {
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&taskRecord{}).Where("id = ? AND status = ?", id, task.StatusOpen).Updates(map[string]any{
			"worker_id": workerID, "status": task.StatusClaimed,
		})
		if result.Error != nil {
			return fmt.Errorf("claim task: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&taskRecord{}).Where("id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return domain.ErrNotFound
			}
			return fmt.Errorf("%w: task is no longer open", domain.ErrConflict)
		}
		// The service checks these rules before calling Claim, but two
		// concurrent claims for the same rider can both pass that check.
		// Re-check under the rider row lock so the task update is rolled back
		// if this claim would exceed the rider's capacity.
		var rider riderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", workerID).Take(&rider).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		} else {
			if rider.Archived {
				return domain.ErrForbidden
			}
			if rider.Status != "AVAILABLE" {
				return fmt.Errorf("%w: 请先把接单状态切换为上线", domain.ErrConflict)
			}
			var active int64
			if err := tx.Model(&taskRecord{}).
				Where("worker_id = ? AND status IN ?", workerID, []string{task.StatusClaimed, task.StatusArrived, task.StatusProcessing}).
				Count(&active).Error; err != nil {
				return err
			}
			if rider.MaxConcurrentTasks > 0 && active > int64(rider.MaxConcurrentTasks) {
				return fmt.Errorf("%w: 同时进行的任务已达上限（%d 单）", domain.ErrConflict, rider.MaxConcurrentTasks)
			}
		}
		from := task.StatusOpen
		event := taskEventRecord{TaskID: id, From: &from, To: task.StatusClaimed, ActorID: optionalString(workerID)}
		return tx.Create(&event).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Store) UpdateStatus(ctx context.Context, id, next, actorID string) (*task.Task, error) {
	return s.updateStatus(ctx, id, next, actorID, "", false)
}

func (s *Store) Cancel(ctx context.Context, id, actorID, reason string) (*task.Task, error) {
	return s.updateStatus(ctx, id, task.StatusCancelled, actorID, reason, false)
}

// updateStatus is the single transaction boundary for task transitions. When
// enqueue is true, COMPLETED/CANCELLED also writes an integration event to the
// same transaction as the status and task_events row. That makes the outbox
// the durable acceptance point for settlement and refund work.
func (s *Store) updateStatus(ctx context.Context, id, next, actorID, reason string, enqueue bool) (*task.Task, error) {
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record taskRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("lock task: %w", err)
		}
		if !task.ValidTransition(record.Status, next) {
			return fmt.Errorf("%w: invalid status transition: %s -> %s", domain.ErrConflict, record.Status, next)
		}
		if next == task.StatusCompleted && !task.PickupsComplete(record.PickupStops) {
			return fmt.Errorf("%w: 请先完成所有取件点", domain.ErrConflict)
		}
		now := time.Now().UTC()
		updates := map[string]any{"status": next, "updated_at": now}
		if next == task.StatusCancelled {
			reason = strings.TrimSpace(reason)
			if reason == "" {
				reason = "任务取消"
			}
			updates["cancelled_at"] = now
			updates["cancel_reason"] = reason
			record.CancelledAt = &now
			record.CancelReason = reason
		}
		if next == task.StatusCompleted {
			updates["completed_at"] = now
			record.CompletedAt = &now
		}
		if err := tx.Model(&taskRecord{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		// rider_profiles is a read-only view in the Task schema. Completed-count
		// updates belong to the future rider/identity event flow, not a direct
		// cross-service write from Task.
		from := record.Status
		event := taskEventRecord{TaskID: id, From: &from, To: next, ActorID: optionalString(actorID)}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		if enqueue && (next == task.StatusCompleted || next == task.StatusCancelled) {
			record.Status = next
			record.UpdatedAt = now
			payload, err := json.Marshal(toTask(record))
			if err != nil {
				return err
			}
			eventID, err := newUUID()
			if err != nil {
				return err
			}
			outbox := outboxRecord{
				ID: eventID, EventType: taskEventType(next), AggregateID: id,
				Payload: string(payload), Status: "PENDING", AvailableAt: now,
				CreatedAt: now,
			}
			if err := tx.Table("task_outbox").Create(&outbox).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func hasArrivedStop(stops []task.PickupStop) bool {
	for _, stop := range stops {
		if stop.Status == "ARRIVED" {
			return true
		}
	}
	return false
}

func allStopsPicked(stops []task.PickupStop) bool {
	if len(stops) == 0 {
		return false
	}
	for _, stop := range stops {
		if stop.Status != "PICKED_UP" {
			return false
		}
	}
	return true
}

func taskEventType(status string) string {
	switch status {
	case task.StatusCompleted:
		return "task.completed.v1"
	case task.StatusCancelled:
		return "task.cancelled.v1"
	default:
		return "task.status-changed.v1"
	}
}

func (s *Store) AdvancePickup(ctx context.Context, id, actor string, index int, next string) (*task.Task, error) {
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record taskRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if stringValue(record.WorkerID) != actor {
			return domain.ErrForbidden
		}
		if record.Status != task.StatusClaimed && record.Status != task.StatusArrived && record.Status != task.StatusProcessing {
			return domain.ErrConflict
		}
		if err := task.AdvancePickup(record.PickupStops, index, next); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrConflict, err)
		}
		encoded, err := json.Marshal(record.PickupStops)
		if err != nil {
			return err
		}
		updates := map[string]any{"pickup_stops": string(encoded)}
		nextStatus := ""
		// Derive the aggregate status from the stops themselves. A single-stop
		// order that is picked up may skip ARRIVED and go straight to PROCESSING;
		// a multi-stop order arrives first, then processes once every stop is done.
		if allStopsPicked(record.PickupStops) {
			nextStatus = task.StatusProcessing
		} else if hasArrivedStop(record.PickupStops) {
			nextStatus = task.StatusArrived
		}
		if nextStatus == record.Status {
			nextStatus = ""
		}
		if nextStatus != "" {
			updates["status"] = nextStatus
			updates["updated_at"] = time.Now().UTC()
		}
		if err = tx.Model(&taskRecord{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Create(&taskEventRecord{TaskID: id, To: fmt.Sprintf("PICKUP_%d_%s", index+1, next), ActorID: optionalString(actor)}).Error; err != nil {
			return err
		}
		if nextStatus != "" {
			from := record.Status
			if err := tx.Create(&taskEventRecord{TaskID: id, From: &from, To: nextStatus, ActorID: optionalString(actor)}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Store) loadEvents(ctx context.Context, item *task.Task) error {
	var records []taskEventRecord
	if err := s.db.ORM.WithContext(ctx).Where("task_id = ?", item.ID).Order("id").Find(&records).Error; err != nil {
		return fmt.Errorf("load task events: %w", err)
	}
	item.Events = make([]task.TaskEvent, 0, len(records))
	for _, record := range records {
		item.Events = append(item.Events, toTaskEvent(record))
	}
	return nil
}

func (s *Store) loadEventsForTasks(ctx context.Context, items []*task.Task) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[string]*task.Task, len(items))
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
		item.Events = make([]task.TaskEvent, 0)
		byID[item.ID] = item
	}
	var records []taskEventRecord
	if err := s.db.ORM.WithContext(ctx).Where("task_id IN ?", ids).Order("id").Find(&records).Error; err != nil {
		return fmt.Errorf("load task events: %w", err)
	}
	for _, record := range records {
		byID[record.TaskID].Events = append(byID[record.TaskID].Events, toTaskEvent(record))
	}
	return nil
}

func (s *Store) CountActiveForRider(ctx context.Context, riderID string) (int, error) {
	var count int64
	if err := s.db.ORM.WithContext(ctx).Model(&taskRecord{}).
		Where("worker_id = ? AND status IN ?", riderID, []string{task.StatusClaimed, task.StatusArrived, task.StatusProcessing}).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *Store) CreateIncident(ctx context.Context, incident task.Incident) (*task.Incident, error) {
	images := incident.ImageURLs
	if images == nil {
		images = []string{}
	}
	record := taskIncidentRecord{
		ID: incident.ID, TaskID: incident.TaskID, ReporterID: incident.ReporterID, ReporterRole: incident.ReporterRole,
		Kind: incident.Kind, Note: incident.Note, ImageURLs: images, Status: task.IncidentOpen, CreatedAt: incident.CreatedAt,
	}
	if err := s.db.ORM.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return &incident, nil
}

func (s *Store) ListIncidents(ctx context.Context, taskID string) ([]task.Incident, error) {
	var records []taskIncidentRecord
	if err := s.db.ORM.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at").Order("id").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]task.Incident, 0, len(records))
	for _, record := range records {
		out = append(out, toIncident(record))
	}
	return out, nil
}

func (s *Store) ListIncidentsByStatus(ctx context.Context, status string, page repository.Page) ([]task.Incident, error) {
	query := s.db.ORM.WithContext(ctx).Model(&taskIncidentRecord{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var records []taskIncidentRecord
	if err := query.Order("created_at DESC").Order("id DESC").Limit(page.Limit).Offset(page.Offset).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]task.Incident, 0, len(records))
	for _, record := range records {
		out = append(out, toIncident(record))
	}
	return out, nil
}

func (s *Store) ResolveIncident(ctx context.Context, taskID, incidentID, resolverID, resolution string, at time.Time) (*task.Incident, error) {
	result := s.db.ORM.WithContext(ctx).Model(&taskIncidentRecord{}).
		Where("id = ? AND task_id = ? AND status = ?", incidentID, taskID, task.IncidentOpen).
		Updates(map[string]any{"status": task.IncidentResolved, "resolution": resolution, "resolved_by": resolverID, "resolved_at": at})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := s.db.ORM.WithContext(ctx).Model(&taskIncidentRecord{}).Where("id = ? AND task_id = ?", incidentID, taskID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, domain.ErrNotFound
		}
		return nil, domain.ErrConflict
	}
	incidents, err := s.ListIncidents(ctx, taskID)
	if err != nil {
		return nil, err
	}
	for _, item := range incidents {
		if item.ID == incidentID {
			return &item, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) loadIncidents(ctx context.Context, item *task.Task) error {
	incidents, err := s.ListIncidents(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("load task incidents: %w", err)
	}
	item.Incidents = incidents
	return nil
}

func (s *Store) loadIncidentsForTasks(ctx context.Context, items []*task.Task) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	var records []taskIncidentRecord
	if err := s.db.ORM.WithContext(ctx).Where("task_id IN ?", ids).Order("created_at ASC").Order("id").Find(&records).Error; err != nil {
		return fmt.Errorf("list task incidents: %w", err)
	}
	byTask := make(map[string][]task.Incident, len(items))
	for _, record := range records {
		byTask[record.TaskID] = append(byTask[record.TaskID], toIncident(record))
	}
	for _, item := range items {
		item.Incidents = byTask[item.ID]
	}
	return nil
}
