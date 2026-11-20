package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/payment"
	"cityflow/internal/domain/task"
)

// PaymentGate is the optional payment policy the task workflow consults. It
// lives here as an interface so the task domain never imports provider code.
type PaymentGate interface {
	EnsureClaimable(context.Context, *task.Task) error
	EnsureEditable(context.Context, *task.Task, *int64) error
	AfterCancel(context.Context, *task.Task)
}

type SettlementTrigger interface {
	EnsureSettlement(context.Context, *task.Task) (*payment.Settlement, error)
}

type TaskService struct {
	tasks       repository.TaskRepository
	trusted     *TrustedPlaces
	toWGS       func(location.Point) location.Point
	paymentGate PaymentGate
	settlements SettlementTrigger
	outbox      repository.TaskOutboxRepository
	area        interface {
		Validate(context.Context, []location.Point) error
	}
}

// WithOutbox switches status transitions to the transactional outbox. On
// MySQL this is the production path: the status change and event commit
// together, and settlement/refund happen asynchronously after publishing.
func (s *TaskService) WithOutbox(outbox repository.TaskOutboxRepository) *TaskService {
	s.outbox = outbox
	return s
}

// WithPaymentGate installs payment checks without changing the constructor, so
// existing tests keep building an unpaid task flow.
func (s *TaskService) WithPaymentGate(gate PaymentGate) *TaskService {
	s.paymentGate = gate
	return s
}

func (s *TaskService) WithSettlements(trigger SettlementTrigger) *TaskService {
	s.settlements = trigger
	return s
}

func (s *TaskService) WithTrustedPlaces(places TrustedPlaces, toWGS func(location.Point) location.Point) *TaskService {
	s.trusted = &places
	s.toWGS = toWGS
	return s
}

func (s *TaskService) resolvePlaces(ctx context.Context, stops []task.PickupStop, destinationID string) ([]task.PickupStop, string, *location.Point, error) {
	if len(stops) == 0 || len(stops) > 50 {
		return nil, "", nil, fmt.Errorf("%w: 请选择取件地点", domain.ErrInvalidRequest)
	}
	ids := make([]string, 0, len(stops)+1)
	for _, stop := range stops {
		ids = append(ids, stop.POIID)
	}
	ids = append(ids, destinationID)
	places, err := s.trusted.Resolve(ctx, ids)
	if err != nil {
		return nil, "", nil, err
	}
	resolved := append([]task.PickupStop(nil), stops...)
	for i := range resolved {
		p := s.toWGS(places[i].Location)
		resolved[i].Address = places[i].Name
		if places[i].Address != "" && places[i].Address != places[i].Name {
			resolved[i].Address += " · " + places[i].Address
		}
		resolved[i].Location = &p
	}
	last := places[len(places)-1]
	p := s.toWGS(last.Location)
	address := last.Name
	if last.Address != "" && last.Address != last.Name {
		address += " · " + last.Address
	}
	return resolved, address, &p, nil
}

func (s *TaskService) WithServiceArea(area interface {
	Validate(context.Context, []location.Point) error
}) *TaskService {
	s.area = area
	return s
}

func (s *TaskService) validateArea(ctx context.Context, stops []task.PickupStop, pickup, destination *location.Point) error {
	if s.area == nil {
		return nil
	}
	points := make([]location.Point, 0, len(stops)+1)
	if len(stops) == 0 {
		if pickup == nil {
			return fmt.Errorf("%w: 请选择取件地点", domain.ErrInvalidRequest)
		}
		points = append(points, *pickup)
	} else {
		for _, stop := range stops {
			if stop.Location == nil {
				return fmt.Errorf("%w: 请选择每个取件地点", domain.ErrInvalidRequest)
			}
			points = append(points, *stop.Location)
		}
	}
	if destination == nil {
		return fmt.Errorf("%w: 请选择送达地点", domain.ErrInvalidRequest)
	}
	points = append(points, *destination)
	return s.area.Validate(ctx, points)
}

func NewTaskService(tasks repository.TaskRepository) *TaskService {
	return &TaskService{tasks: tasks}
}

func (s *TaskService) Create(ctx context.Context, request task.CreateRequest, createdBy string) (*task.Task, error) {
	if request.IdempotencyKey != "" {
		if len(request.IdempotencyKey) > 128 || strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey {
			return nil, fmt.Errorf("%w: invalid idempotency key", domain.ErrInvalidRequest)
		}
		payload, err := json.Marshal(request)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(payload)
		request.IdempotencyHash = hex.EncodeToString(sum[:])
		if existing, err := s.tasks.FindIdempotent(ctx, createdBy, request.IdempotencyKey, request.IdempotencyHash); err == nil {
			return existing, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	if s.trusted != nil {
		stops, address, point, err := s.resolvePlaces(ctx, request.PickupStops, request.DestinationPOIID)
		if err != nil {
			return nil, err
		}
		request.PickupStops = stops
		request.Destination = address
		request.DestinationLocation = point
	}
	request = task.NormalizeCreateRequest(request)
	if err := task.ValidateCreateRequest(request); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidRequest, err)
	}

	if s.trusted == nil {
		if err := s.validateArea(ctx, request.PickupStops, request.PickupLocation, request.DestinationLocation); err != nil {
			return nil, err
		}
	}
	return s.tasks.Create(ctx, request, createdBy)
}

func (s *TaskService) ListForUser(ctx context.Context, userID, status string, page repository.Page) ([]*task.Task, error) {
	return s.tasks.ListByCreator(ctx, userID, status, page)
}

func (s *TaskService) ListForRider(ctx context.Context, riderID, status string, page repository.Page) ([]*task.Task, error) {
	return s.tasks.ListForRider(ctx, riderID, status, page)
}

func (s *TaskService) ListAll(ctx context.Context, status string, page repository.Page) ([]*task.Task, error) {
	return s.tasks.List(ctx, status, page)
}

// CountActiveForRider exposes the repository's active-order count so rider
// onboarding/profile limits can be enforced before a task is claimed.
func (s *TaskService) CountActiveForRider(ctx context.Context, riderID string) (int, error) {
	return s.tasks.CountActiveForRider(ctx, riderID)
}

func (s *TaskService) Get(ctx context.Context, id string) (*task.Task, error) {
	return s.tasks.Get(ctx, id)
}

func (s *TaskService) GetVisible(ctx context.Context, id string, claims auth.Claims) (*task.Task, error) {
	item, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if claims.Role == auth.RoleAdmin || item.CreatedBy == claims.Subject || item.WorkerID == claims.Subject {
		return item, nil
	}
	// A rider may inspect the open pool, but only in its redacted form: contact
	// details and the creator's identity become available after a successful claim.
	if claims.Role == auth.RoleRider && item.Status == task.StatusOpen {
		return item.PoolView(), nil
	}
	return nil, domain.ErrForbidden
}

func (s *TaskService) Claim(ctx context.Context, id, riderID string) (*task.Task, error) {
	if s.paymentGate != nil {
		item, err := s.tasks.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if item.Status != task.StatusOpen {
			return nil, fmt.Errorf("%w: task is no longer open", domain.ErrConflict)
		}
		if err := s.paymentGate.EnsureClaimable(ctx, item); err != nil {
			return nil, err
		}
	}
	return s.tasks.Claim(ctx, id, riderID)
}

func (s *TaskService) AdvancePickup(ctx context.Context, id, actor string, index int, next string) (*task.Task, error) {
	return s.tasks.AdvancePickup(ctx, id, actor, index, next)
}

func (s *TaskService) UpdateStatus(ctx context.Context, id, next, actorID string, role string) (*task.Task, error) {
	current, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && current.WorkerID != actorID {
		return nil, domain.ErrForbidden
	}
	return s.transition(ctx, id, strings.ToUpper(strings.TrimSpace(next)), actorID, true)
}

// transition writes the status change. With an outbox configured the
// integration event is committed atomically and Payment handles settlement or
// refund later; the direct call below exists only for memory/test stores.
func (s *TaskService) transition(ctx context.Context, id, next, actorID string, settle bool) (*task.Task, error) {
	if s.outbox != nil {
		return s.outbox.UpdateStatusWithOutbox(ctx, id, next, actorID)
	}
	updated, err := s.tasks.UpdateStatus(ctx, id, next, actorID)
	if err != nil {
		return nil, err
	}
	if settle && s.settlements != nil && updated.Status == task.StatusCompleted {
		if _, err := s.settlements.EnsureSettlement(ctx, updated); err != nil {
			log.Printf("warning: create settlement for task %s: %v", updated.ID, err)
		}
	}
	return updated, nil
}

func (s *TaskService) Update(ctx context.Context, id string, request task.UpdateRequest, actorID, role string) (*task.Task, error) {
	current, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && current.CreatedBy != actorID {
		return nil, domain.ErrForbidden
	}
	if current.Status != task.StatusOpen {
		return nil, fmt.Errorf("%w: only open tasks can be edited", domain.ErrConflict)
	}
	if s.paymentGate != nil {
		nextAmount := request.QuotedAmountFen
		if nextAmount == nil {
			nextAmount = current.QuotedAmountFen
		}
		if err := s.paymentGate.EnsureEditable(ctx, current, nextAmount); err != nil {
			return nil, err
		}
	}
	// The task editor does not yet expose the quote, so an omitted value keeps it.
	if request.QuotedAmountFen == nil {
		request.QuotedAmountFen = current.QuotedAmountFen
	}
	if request.ContactName == "" && request.ContactPhone == "" && request.HandoverNote == "" {
		request.ContactName, request.ContactPhone, request.HandoverNote = current.ContactName, current.ContactPhone, current.HandoverNote
	}
	if s.trusted != nil {
		stops, address, point, err := s.resolvePlaces(ctx, request.PickupStops, request.DestinationPOIID)
		if err != nil {
			return nil, err
		}
		request.PickupStops = stops
		request.Destination = address
		request.DestinationLocation = point
	}
	legacyUpdate := request.PickupStops == nil
	if legacyUpdate {
		request.PickupStops = append([]task.PickupStop(nil), current.PickupStops...)
		if len(request.PickupStops) > 0 {
			request.PickupStops[0].Address = request.Pickup
			request.PickupStops[0].Location = request.PickupLocation
		}
	}
	if request.DeliveryAt == nil && legacyUpdate {
		request.DeliveryAt = current.DeliveryAt
	}
	normalized := task.NormalizeUpdateRequest(request)
	if err := task.ValidateUpdateRequest(normalized); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidRequest, err)
	}
	// Preserve editing of legacy tasks with unchanged locations; new location
	// selections always pass the current service-area policy.
	if s.trusted == nil && (!samePickupPlaces(current.PickupStops, normalized.PickupStops) || !reflect.DeepEqual(current.PickupLocation, normalized.PickupLocation) || !reflect.DeepEqual(current.DestinationLocation, normalized.DestinationLocation) || current.Pickup != normalized.Pickup || current.Destination != normalized.Destination) {
		if err := s.validateArea(ctx, normalized.PickupStops, normalized.PickupLocation, normalized.DestinationLocation); err != nil {
			return nil, err
		}
	}
	return s.tasks.Update(ctx, id, normalized)
}

func samePickupPlaces(a, b []task.PickupStop) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Address != b[i].Address || !reflect.DeepEqual(a[i].Location, b[i].Location) {
			return false
		}
	}
	return true
}

func (s *TaskService) CreateIncident(ctx context.Context, taskID, actorID, role string, req task.IncidentRequest) (*task.Incident, error) {
	current, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if current.Status == task.StatusCompleted || current.Status == task.StatusCancelled {
		return nil, fmt.Errorf("%w: 订单已结束，不能再上报异常", domain.ErrConflict)
	}
	creator := current.CreatedBy == actorID
	rider := current.WorkerID == actorID && role == auth.RoleRider
	if role != auth.RoleAdmin && !creator && !rider {
		return nil, domain.ErrForbidden
	}
	req.Kind = strings.ToUpper(strings.TrimSpace(req.Kind))
	req.Note = strings.TrimSpace(req.Note)
	if !task.ValidIncidentKind(req.Kind) || len([]rune(req.Note)) > 500 {
		return nil, fmt.Errorf("%w: 异常类型或说明无效", domain.ErrInvalidRequest)
	}
	if len(req.ImageURLs) > 6 {
		return nil, fmt.Errorf("%w: 最多附加六张图片", domain.ErrInvalidRequest)
	}
	for _, raw := range req.ImageURLs {
		raw = strings.TrimSpace(raw)
		local := strings.HasPrefix(raw, "/uploads/") && !strings.Contains(raw, "..") && !strings.ContainsAny(raw, "?#\r\n")
		remote := strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
		if len(raw) > 2048 || !(local || remote) {
			return nil, fmt.Errorf("%w: 图片链接无效", domain.ErrInvalidRequest)
		}
	}
	if req.Kind == task.IncidentUserCancel && !creator && role != auth.RoleAdmin {
		return nil, domain.ErrForbidden
	}
	if (req.Kind == task.IncidentRiderCancel || req.Kind == task.IncidentContactFailed || req.Kind == task.IncidentPickupFailed || req.Kind == task.IncidentAddressWrong) && !rider && role != auth.RoleAdmin {
		return nil, domain.ErrForbidden
	}
	for _, existing := range current.Incidents {
		if existing.Status == task.IncidentOpen && existing.Kind == req.Kind && existing.ReporterID == actorID {
			return nil, fmt.Errorf("%w: 同类异常已在等待平台处理", domain.ErrConflict)
		}
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	incident := task.Incident{
		ID: hex.EncodeToString(key[:]), TaskID: taskID, ReporterID: actorID, ReporterRole: role,
		Kind: req.Kind, Note: req.Note, ImageURLs: append([]string{}, req.ImageURLs...), Status: task.IncidentOpen, CreatedAt: time.Now().UTC(),
	}
	return s.tasks.CreateIncident(ctx, incident)
}

func (s *TaskService) ListIncidents(ctx context.Context, taskID string) ([]task.Incident, error) {
	return s.tasks.ListIncidents(ctx, taskID)
}

func (s *TaskService) ListIncidentsForPlatform(ctx context.Context, status string, page repository.Page) ([]task.Incident, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "" && status != task.IncidentOpen && status != task.IncidentResolved {
		return nil, fmt.Errorf("%w: 异常状态无效", domain.ErrInvalidRequest)
	}
	if page.Limit <= 0 || page.Limit > 200 || page.Offset < 0 {
		return nil, fmt.Errorf("%w: 分页参数无效", domain.ErrInvalidRequest)
	}
	return s.tasks.ListIncidentsByStatus(ctx, status, page)
}

func (s *TaskService) ResolveIncident(ctx context.Context, taskID, incidentID, adminID, resolution string, cancel bool) (*task.Task, error) {
	resolution = strings.TrimSpace(resolution)
	if resolution == "" || len([]rune(resolution)) > 500 {
		return nil, fmt.Errorf("%w: 请填写处理结果", domain.ErrInvalidRequest)
	}
	if cancel {
		current, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if !task.ValidTransition(current.Status, task.StatusCancelled) {
			return nil, fmt.Errorf("%w: 当前履约阶段不能直接取消", domain.ErrConflict)
		}
	}
	if _, err := s.tasks.ResolveIncident(ctx, taskID, incidentID, adminID, resolution, time.Now().UTC()); err != nil {
		return nil, err
	}
	if cancel {
		return s.Cancel(ctx, taskID, adminID, auth.RoleAdmin)
	}
	return s.Get(ctx, taskID)
}

func (s *TaskService) Cancel(ctx context.Context, id, actorID, role string) (*task.Task, error) {
	current, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && current.CreatedBy != actorID {
		return nil, domain.ErrForbidden
	}
	updated, err := s.transition(ctx, id, task.StatusCancelled, actorID, false)
	if err != nil {
		return nil, err
	}
	if s.outbox == nil && s.paymentGate != nil && updated.Status == task.StatusCancelled {
		// Legacy in-memory path: no durable outbox exists, so keep the old
		// fire-and-forget refund. MySQL uses task.cancelled.v1 instead.
		go s.paymentGate.AfterCancel(context.WithoutCancel(ctx), updated)
	}
	return updated, nil
}
