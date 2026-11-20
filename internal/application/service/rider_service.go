package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
)

type RiderService struct {
	profiles  repository.RiderRepository
	accounts  accountReader
	positions positionReader
	tasks     *TaskService
}

func NewRiderService(profiles repository.RiderRepository, accounts accountReader, tasks *TaskService) *RiderService {
	return &RiderService{profiles: profiles, accounts: accounts, tasks: tasks}
}

func (s *RiderService) WithPositions(positions positionReader) *RiderService {
	s.positions = positions
	return s
}
func (s *RiderService) Profile(ctx context.Context, riderID string) (*rider.Profile, error) {
	return s.ensureProfile(ctx, riderID)
}

// ensureProfile repairs the one gap left by admin provisioning/demo seeding:
// a RIDER account can exist before Task has a rider_profiles row. Reading or
// listing tasks must not fail with 404 in that state, otherwise the rider
// workbench cannot even show the "go online" control.
func (s *RiderService) ensureProfile(ctx context.Context, riderID string) (*rider.Profile, error) {
	profile, err := s.profiles.RiderForUser(ctx, riderID)
	if err == nil {
		return profile, nil
	}
	if !errors.Is(err, domain.ErrNotFound) || s.accounts == nil {
		return nil, err
	}
	account, err := s.accounts.GetUser(ctx, riderID)
	if err != nil {
		return nil, err
	}
	if account.Role != auth.RoleRider || account.Status != "ACTIVE" {
		return nil, domain.ErrNotFound
	}
	displayName := strings.TrimSpace(account.Name)
	if displayName == "" {
		displayName = strings.SplitN(account.Email, "@", 2)[0]
	}
	request := rider.UpdateRequest{
		DisplayName: displayName, VehicleType: "E_BIKE", Skills: []string{"DELIVERY"},
		MaxDistanceMeters: 5000, MaxConcurrentTasks: 3,
	}
	created, err := s.Create(ctx, riderID, request)
	if err != nil {
		// Concurrent first reads can race on the unique user_id. Re-read before
		// returning an error so a losing request behaves like the winner.
		if current, readErr := s.profiles.RiderForUser(ctx, riderID); readErr == nil {
			return current, nil
		}
		return nil, err
	}
	updated, statusErr := s.profiles.SetRiderStatus(ctx, riderID, "AVAILABLE")
	if statusErr != nil {
		return created, nil
	}
	return updated, nil
}
func (s *RiderService) ListTasks(ctx context.Context, riderID, status string, page repository.Page) ([]*task.Task, error) {
	return s.tasks.ListForRider(ctx, riderID, status, page)
}

// ListOpenTasks returns only tasks whose nearest pending pickup is inside the
// rider's configured service radius. The backend owns this filter; the browser
// only renders the result.
func (s *RiderService) ListOpenTasks(ctx context.Context, riderID string, point *location.Point, page repository.Page) ([]*task.Task, error) {
	profile, err := s.ensureProfile(ctx, riderID)
	if err != nil {
		return nil, err
	}
	if profile.Status != "AVAILABLE" {
		return []*task.Task{}, nil
	}
	if point == nil && s.positions != nil {
		stored, err := s.positions.GetPosition(ctx, riderID)
		if err == nil {
			point = &stored.Point
		} else if err != domain.ErrNotFound {
			return nil, err
		}
	}
	items, err := s.tasks.ListAll(ctx, task.StatusOpen, repository.Page{Limit: 200})
	if err != nil {
		return nil, err
	}
	type ranked = rankedTask
	candidates := make([]ranked, 0, len(items))
	for _, item := range items {
		distance, ok := nearestPickupDistance(item, point)
		if !ok {
			// Only legacy tasks with no pending pickup coordinates stay visible
			// without a rider location. Any coordinate-bearing task must pass the
			// rider's configured service radius.
			if !hasPendingPickupPoint(item) {
				candidates = append(candidates, ranked{item: item, distance: math.Inf(1)})
			}
			continue
		}
		if distance <= float64(profile.MaxDistanceMeters) {
			candidates = append(candidates, ranked{item: item, distance: distance})
		}
	}
	sortRankedTasks(candidates)
	result := make([]*task.Task, 0, len(candidates))
	for _, candidate := range candidates {
		// The open pool is shared with every available rider, so it only ever
		// carries the redacted view; full details follow a successful claim.
		result = append(result, candidate.item.PoolView())
	}
	if page.Offset >= len(result) {
		return []*task.Task{}, nil
	}
	end := page.Offset + page.Limit
	if end > len(result) {
		end = len(result)
	}
	return result[page.Offset:end], nil
}

type rankedTask struct {
	item     *task.Task
	distance float64
}

func sortRankedTasks(items []rankedTask) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].distance == items[j].distance {
			return items[i].item.CreatedAt.After(items[j].item.CreatedAt)
		}
		return items[i].distance < items[j].distance
	})
}

func nearestPickupDistance(item *task.Task, point *location.Point) (float64, bool) {
	if point == nil {
		return 0, false
	}
	best := math.Inf(1)
	found := false
	stops := item.PickupStops
	if len(stops) == 0 && item.PickupLocation != nil {
		stops = []task.PickupStop{{Location: item.PickupLocation}}
	}
	for _, stop := range stops {
		if stop.Location == nil || stop.Status == "PICKED_UP" {
			continue
		}
		distance := pointDistance(*point, *stop.Location)
		if distance < best {
			best = distance
			found = true
		}
	}
	return best, found
}

func hasPendingPickupPoint(item *task.Task) bool {
	if len(item.PickupStops) == 0 {
		return item.PickupLocation != nil
	}
	for _, stop := range item.PickupStops {
		if stop.Status != "PICKED_UP" && stop.Location != nil {
			return true
		}
	}
	return false
}

func pointDistance(a, b location.Point) float64 {
	const earthRadius = 6371000.0
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	dLat := lat2 - lat1
	dLon := (b.Longitude - a.Longitude) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(h)))
}

func (s *RiderService) List(ctx context.Context, status string, page repository.Page) ([]*rider.Profile, error) {
	return s.profiles.ListRiders(ctx, strings.ToUpper(strings.TrimSpace(status)), page)
}

func (s *RiderService) Create(ctx context.Context, userID string, request rider.UpdateRequest) (*rider.Profile, error) {
	account, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if account.Role != auth.RoleRider {
		return nil, domain.ErrForbidden
	}
	if account.Status != "ACTIVE" {
		return nil, domain.ErrUnauthorized
	}
	request, err = normalizeRiderRequest(request)
	if err != nil {
		return nil, err
	}
	return s.profiles.CreateRider(ctx, userID, request)
}

func (s *RiderService) Update(ctx context.Context, userID string, request rider.UpdateRequest) (*rider.Profile, error) {
	account, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if account.Role != auth.RoleRider {
		return nil, domain.ErrForbidden
	}
	if account.Status != "ACTIVE" {
		return nil, domain.ErrUnauthorized
	}
	request, err = normalizeRiderRequest(request)
	if err != nil {
		return nil, err
	}
	return s.profiles.UpdateRider(ctx, userID, request)
}

func (s *RiderService) Archive(ctx context.Context, userID string) error {
	return s.profiles.ArchiveRider(ctx, userID)
}

func normalizeRiderRequest(request rider.UpdateRequest) (rider.UpdateRequest, error) {
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.VehicleType = strings.ToUpper(strings.TrimSpace(request.VehicleType))
	request.ServiceArea = strings.TrimSpace(request.ServiceArea)
	if request.DisplayName == "" || request.MaxDistanceMeters <= 0 || request.MaxConcurrentTasks <= 0 || request.MaxConcurrentTasks > 20 {
		return request, fmt.Errorf("%w: display_name and positive distance/task limits are required", domain.ErrInvalidRequest)
	}
	switch request.VehicleType {
	case "WALK", "BICYCLE", "E_BIKE", "CAR", "OTHER":
	default:
		return request, fmt.Errorf("%w: unsupported vehicle_type", domain.ErrInvalidRequest)
	}
	request.Skills = normalizeTypes(request.Skills)
	if len(request.Skills) == 0 {
		return request, fmt.Errorf("%w: at least one service skill is required", domain.ErrInvalidRequest)
	}
	return request, nil
}
func (s *RiderService) SetAvailability(ctx context.Context, userID, status string) (*rider.Profile, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "AVAILABLE" && status != "OFFLINE" {
		return nil, fmt.Errorf("%w: 状态只能是 AVAILABLE 或 OFFLINE", domain.ErrInvalidRequest)
	}
	account, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if account.Role != auth.RoleRider || account.Status != "ACTIVE" {
		return nil, domain.ErrForbidden
	}
	if _, err := s.ensureProfile(ctx, userID); err != nil {
		return nil, err
	}
	return s.profiles.SetRiderStatus(ctx, userID, status)
}

func (s *RiderService) Claim(ctx context.Context, taskID, riderID string) (*task.Task, error) {
	profile, err := s.ensureProfile(ctx, riderID)
	if err != nil {
		return nil, err
	}
	if profile.Status == "SUSPENDED" {
		return nil, domain.ErrForbidden
	}
	if profile.Status != "AVAILABLE" {
		return nil, fmt.Errorf("%w: 请先把接单状态切换为上线", domain.ErrConflict)
	}
	active, err := s.tasks.CountActiveForRider(ctx, riderID)
	if err != nil {
		return nil, err
	}
	if profile.MaxConcurrentTasks > 0 && active >= profile.MaxConcurrentTasks {
		return nil, fmt.Errorf("%w: 同时进行的任务已达上限（%d 单）", domain.ErrConflict, profile.MaxConcurrentTasks)
	}
	item, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var riderPoint *location.Point
	if s.positions != nil {
		position, err := s.positions.GetPosition(ctx, riderID)
		if err == nil {
			riderPoint = &position.Point
		} else if err != domain.ErrNotFound {
			return nil, err
		}
	}
	if distance, hasPickup := nearestPickupDistance(item, riderPoint); hasPickup {
		if distance > float64(profile.MaxDistanceMeters) {
			return nil, fmt.Errorf("%w: 取件点距离 %.0f 米，超过服务范围 %d 米", domain.ErrConflict, distance, profile.MaxDistanceMeters)
		}
	} else if hasPendingPickupPoint(item) {
		return nil, fmt.Errorf("%w: 接单前需要先上报当前位置", domain.ErrConflict)
	}
	return s.tasks.Claim(ctx, taskID, riderID)
}
func (s *RiderService) UpdateTaskStatus(ctx context.Context, taskID, status, riderID, role string) (*task.Task, error) {
	return s.tasks.UpdateStatus(ctx, taskID, status, riderID, role)
}

func (s *RiderService) AdvancePickup(ctx context.Context, id, actor string, index int, next string) (*task.Task, error) {
	return s.tasks.AdvancePickup(ctx, id, actor, index, next)
}
