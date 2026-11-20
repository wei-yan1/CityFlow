package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/comment"
	"cityflow/internal/domain/coupon"
	"cityflow/internal/domain/feedback"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/payment"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
	"cityflow/internal/security"
)

type idempotentTask struct{ id, hash string }
type Store struct {
	idempotency        map[string]idempotentTask
	paymentIdem        map[string]string
	paymentOrders      map[string]*payment.Order
	settlements        map[string]*payment.Settlement
	payoutAccounts     map[string]*payment.PayoutAccount
	coupons            map[string]*coupon.Coupon
	couponClaims       map[string]*coupon.UserCoupon
	notifyLogs         []payment.NotifyLog
	reviews            map[string]*merchant.Review
	reviewAudits       []reviewAudit
	media              map[string]merchantMedia
	feedback           []feedback.Record
	comments           map[string][]comment.Record
	incidents          map[string]*task.Incident
	passwordResets     map[string]user.PasswordReset
	tracks             []location.TrackPoint
	nextTrack          uint64
	mu                 sync.RWMutex
	nextID             int
	tasks              map[string]*task.Task
	users              map[string]*user.User
	byEmail            map[string]string
	merchants          map[string]*merchant.Profile
	riders             map[string]*rider.Profile
	riderRegistrations map[string]*rider.Registration
}

func NewStore() (*Store, error) {
	s := &Store{idempotency: make(map[string]idempotentTask), tasks: make(map[string]*task.Task), users: make(map[string]*user.User), byEmail: make(map[string]string), merchants: make(map[string]*merchant.Profile), riders: make(map[string]*rider.Profile), paymentIdem: make(map[string]string), paymentOrders: make(map[string]*payment.Order), settlements: make(map[string]*payment.Settlement), payoutAccounts: make(map[string]*payment.PayoutAccount), coupons: make(map[string]*coupon.Coupon), couponClaims: make(map[string]*coupon.UserCoupon), riderRegistrations: make(map[string]*rider.Registration), incidents: make(map[string]*task.Incident), passwordResets: make(map[string]user.PasswordReset), comments: make(map[string][]comment.Record)}
	now := time.Now().UTC()
	seeds := []struct{ id, email, name, phone, role string }{
		{"00000000-0000-4000-8000-000000000001", "user@cityflow.local", "示例用户", "13800000001", auth.RoleUser},
		{"00000000-0000-4000-8000-000000000002", "merchant@cityflow.local", "示例商家", "13800000002", auth.RoleMerchant},
		{"00000000-0000-4000-8000-000000000003", "rider@cityflow.local", "示例骑手", "13800000003", auth.RoleRider},
		{"00000000-0000-4000-8000-000000000004", "admin@cityflow.local", "平台管理员", "13800000004", auth.RoleAdmin},
	}
	hash, err := security.HashPassword("cityflow123")
	if err != nil {
		return nil, err
	}
	for _, seed := range seeds {
		s.users[seed.id] = &user.User{ID: seed.id, Email: seed.email, Name: seed.name, Phone: seed.phone, Role: seed.role, Status: "ACTIVE", PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
		s.byEmail[seed.email] = seed.id
	}
	serviceTypes := []string{"DELIVERY", "PICKUP"}
	s.merchants[seeds[1].id] = &merchant.Profile{ID: seeds[1].id, UserID: seeds[1].id, Name: "CityFlow 示例商店", Description: "用于演示地点和任务流程的商家", Phone: seeds[1].phone, Address: "校园生活区 1 号店", Latitude: 31.2304, Longitude: 121.4737, BusinessStatus: "OPEN", ServiceTypes: serviceTypes, CapacityPerHour: 60, CreatedAt: now, UpdatedAt: now}
	s.riders[seeds[2].id] = &rider.Profile{ID: seeds[2].id, UserID: seeds[2].id, DisplayName: seeds[2].name, VehicleType: "E_BIKE", Skills: serviceTypes, ServiceArea: "校园生活区", MaxDistanceMeters: 5000, MaxConcurrentTasks: 3, Status: "AVAILABLE", Rating: 5, CreatedAt: now, UpdatedAt: now}
	s.seedCoupons(now)
	return s, nil
}

var _ repository.AuthRepository = (*Store)(nil)
var _ repository.UserRepository = (*Store)(nil)
var _ repository.MerchantRepository = (*Store)(nil)
var _ repository.RiderRepository = (*Store)(nil)
var _ repository.RiderRegistrationRepository = (*Store)(nil)
var _ repository.TaskRepository = (*Store)(nil)
var _ repository.RiderFeedback = (*Store)(nil)
var _ repository.PublicPlaceReviewReader = (*Store)(nil)
var _ repository.PlaceReviewAdmin = (*Store)(nil)

func (s *Store) Ping(context.Context) error { return nil }
func (s *Store) Close() error               { return nil }

func (s *Store) Authenticate(_ context.Context, email, password string) (*user.User, error) {
	s.mu.RLock()
	id := s.byEmail[strings.ToLower(strings.TrimSpace(email))]
	stored := s.users[id]
	if stored == nil {
		s.mu.RUnlock()
		return nil, domain.ErrUnauthorized
	}
	account := *stored
	s.mu.RUnlock()
	if account.Status != "ACTIVE" || !security.VerifyPassword(account.PasswordHash, password) {
		return nil, domain.ErrUnauthorized
	}
	account.PasswordHash = ""
	return &account, nil
}

func (s *Store) GetUser(_ context.Context, id string) (*user.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stored := s.users[id]
	if stored == nil {
		return nil, domain.ErrNotFound
	}
	account := *stored
	account.PasswordHash = ""
	return &account, nil
}

func (s *Store) IsActive(_ context.Context, id string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	account := s.users[id]
	return account != nil && account.Status == "ACTIVE", nil
}

func (s *Store) CreateUser(_ context.Context, request user.CreateRequest) (*user.User, error) {
	hash, err := security.HashPassword(request.Password)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[request.Email]; exists {
		return nil, domain.ErrConflict
	}
	if request.Phone != "" {
		for _, existing := range s.users {
			if existing.Phone == request.Phone {
				return nil, domain.ErrConflict
			}
		}
	}
	s.nextID++
	now := time.Now().UTC()
	item := &user.User{ID: fmt.Sprintf("user-%04d", s.nextID), Email: request.Email, Name: request.Name, Phone: request.Phone, Role: request.Role, Status: "ACTIVE", PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	s.users[item.ID] = item
	s.byEmail[item.Email] = item.ID
	copy := *item
	copy.PasswordHash = ""
	return &copy, nil
}

func (s *Store) UserByEmail(_ context.Context, email string) (*user.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.users[s.byEmail[strings.ToLower(strings.TrimSpace(email))]]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	copy := *item
	copy.PasswordHash = ""
	return &copy, nil
}

func (s *Store) UpdatePassword(_ context.Context, userID, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.users[userID]
	if item == nil {
		return domain.ErrNotFound
	}
	item.PasswordHash = passwordHash
	item.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) CreatePasswordReset(_ context.Context, reset user.PasswordReset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.passwordResets == nil {
		s.passwordResets = map[string]user.PasswordReset{}
	}
	s.passwordResets[reset.TokenHash] = reset
	return nil
}

func (s *Store) PasswordReset(_ context.Context, tokenHash string) (*user.PasswordReset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reset, ok := s.passwordResets[tokenHash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copy := reset
	return &copy, nil
}

func (s *Store) DeletePasswordReset(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.passwordResets, tokenHash)
	return nil
}

func (s *Store) ListUsers(_ context.Context, status string, page repository.Page) ([]*user.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*user.User, 0, len(s.users))
	for _, item := range s.users {
		if status != "" && item.Status != status {
			continue
		}
		copy := *item
		copy.PasswordHash = ""
		result = append(result, &copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return pageUsers(result, page), nil
}

func (s *Store) UpdateUser(_ context.Context, id string, request user.UpdateRequest) (*user.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.users[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if other, exists := s.byEmail[request.Email]; exists && other != id {
		return nil, domain.ErrConflict
	}
	if request.Phone != "" {
		for otherID, existing := range s.users {
			if otherID != id && existing.Phone == request.Phone {
				return nil, domain.ErrConflict
			}
		}
	}
	delete(s.byEmail, item.Email)
	item.Email, item.Name, item.Phone, item.AvatarURL = request.Email, request.Name, request.Phone, request.AvatarURL
	item.UpdatedAt = time.Now().UTC()
	s.byEmail[item.Email] = id
	copy := *item
	copy.PasswordHash = ""
	return &copy, nil
}

func (s *Store) SetUserStatus(_ context.Context, id, status string) (*user.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.users[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	item.Status = status
	item.UpdatedAt = time.Now().UTC()
	copy := *item
	copy.PasswordHash = ""
	return &copy, nil
}

func (s *Store) MerchantForUser(_ context.Context, id string) (*merchant.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := s.merchants[id]
	account := s.users[id]
	if profile == nil || profile.Archived || account == nil || account.Status != "ACTIVE" {
		return nil, domain.ErrNotFound
	}
	copy := *profile
	copy.ServiceTypes = append([]string(nil), profile.ServiceTypes...)
	return &copy, nil
}

func (s *Store) GetMerchant(_ context.Context, id string) (*merchant.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, profile := range s.merchants {
		if profile.ID == id && !profile.Archived && s.users[profile.UserID] != nil && s.users[profile.UserID].Status == "ACTIVE" {
			return cloneMerchant(profile), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) ListMerchants(_ context.Context, status string, page repository.Page) ([]*merchant.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*merchant.Profile, 0, len(s.merchants))
	for _, profile := range s.merchants {
		if profile.Archived || s.users[profile.UserID] == nil || s.users[profile.UserID].Status != "ACTIVE" || (status != "" && profile.BusinessStatus != status) {
			continue
		}
		items = append(items, cloneMerchant(profile))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return pageMerchants(items, page), nil
}

func (s *Store) FindMerchantByAMapPOI(_ context.Context, poiID string) (*merchant.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, profile := range s.merchants {
		if profile.AMAPPOIID == poiID && !profile.Archived && s.users[profile.UserID] != nil && s.users[profile.UserID].Status == "ACTIVE" {
			return cloneMerchant(profile), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) FindMerchantNearName(_ context.Context, name string, point location.Point, radiusMeters float64) (*merchant.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var nearest *merchant.Profile
	nearestDistance := radiusMeters
	for _, profile := range s.merchants {
		if profile.Archived || profile.BusinessStatus != "OPEN" || s.users[profile.UserID] == nil || s.users[profile.UserID].Status != "ACTIVE" || strings.TrimSpace(profile.Name) != strings.TrimSpace(name) {
			continue
		}
		distance := memoryDistanceMeters(point, location.Point{Latitude: profile.Latitude, Longitude: profile.Longitude})
		if distance <= nearestDistance {
			nearest = cloneMerchant(profile)
			nearestDistance = distance
		}
	}
	if nearest == nil {
		return nil, domain.ErrNotFound
	}
	return nearest, nil
}

func memoryDistanceMeters(a, b location.Point) float64 {
	const earthRadius = 6371000.0
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	dLat := lat2 - lat1
	dLon := (b.Longitude - a.Longitude) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(h)))
}

func (s *Store) CreateMerchant(_ context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.merchants[userID]; existing != nil && !existing.Archived {
		return nil, domain.ErrConflict
	}
	s.nextID++
	now := time.Now().UTC()
	item := &merchant.Profile{ID: fmt.Sprintf("merchant-%04d", s.nextID), UserID: userID, Name: request.Name, Description: request.Description, Phone: request.Phone, Address: request.Address, AMAPPOIID: request.AMAPPOIID, Latitude: request.Latitude, Longitude: request.Longitude, BusinessStatus: request.BusinessStatus, ServiceTypes: append([]string(nil), request.ServiceTypes...), CapacityPerHour: request.CapacityPerHour, CreatedAt: now, UpdatedAt: now}
	if previous := s.merchants[userID]; previous != nil {
		item.ID = previous.ID
		item.CreatedAt = previous.CreatedAt
	}
	s.merchants[userID] = item
	return cloneMerchant(item), nil
}

func (s *Store) UpdateMerchant(_ context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.merchants[userID]
	if item == nil || item.Archived {
		return nil, domain.ErrNotFound
	}
	item.Name, item.Description, item.Phone, item.Address = request.Name, request.Description, request.Phone, request.Address
	item.AMAPPOIID, item.Latitude, item.Longitude, item.BusinessStatus = request.AMAPPOIID, request.Latitude, request.Longitude, request.BusinessStatus
	item.ServiceTypes, item.CapacityPerHour = append([]string(nil), request.ServiceTypes...), request.CapacityPerHour
	item.UpdatedAt = time.Now().UTC()
	return cloneMerchant(item), nil
}

func (s *Store) ArchiveMerchant(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.merchants[userID]
	if item == nil || item.Archived {
		return domain.ErrNotFound
	}
	item.Archived, item.BusinessStatus, item.UpdatedAt = true, "CLOSED", time.Now().UTC()
	return nil
}

func (s *Store) RiderForUser(_ context.Context, id string) (*rider.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := s.riders[id]
	account := s.users[id]
	if profile == nil || profile.Archived || account == nil || account.Status != "ACTIVE" {
		return nil, domain.ErrNotFound
	}
	copy := *profile
	copy.Skills = append([]string(nil), profile.Skills...)
	return &copy, nil
}

func (s *Store) GetRider(_ context.Context, id string) (*rider.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, profile := range s.riders {
		if profile.ID == id && !profile.Archived && s.users[profile.UserID] != nil && s.users[profile.UserID].Status == "ACTIVE" {
			return cloneRider(profile), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) ListRiders(_ context.Context, status string, page repository.Page) ([]*rider.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*rider.Profile, 0, len(s.riders))
	for _, profile := range s.riders {
		if profile.Archived || s.users[profile.UserID] == nil || s.users[profile.UserID].Status != "ACTIVE" || (status != "" && profile.Status != status) {
			continue
		}
		items = append(items, cloneRider(profile))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return pageRiders(items, page), nil
}

func (s *Store) CreateRider(_ context.Context, userID string, request rider.UpdateRequest) (*rider.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.riders[userID]; exists {
		return nil, domain.ErrConflict
	}
	s.nextID++
	now := time.Now().UTC()
	item := &rider.Profile{ID: fmt.Sprintf("rider-%04d", s.nextID), UserID: userID, DisplayName: request.DisplayName, VehicleType: request.VehicleType, Skills: append([]string(nil), request.Skills...), ServiceArea: request.ServiceArea, MaxDistanceMeters: request.MaxDistanceMeters, MaxConcurrentTasks: request.MaxConcurrentTasks, Status: "OFFLINE", Rating: 5, CreatedAt: now, UpdatedAt: now}
	s.riders[userID] = item
	return cloneRider(item), nil
}

func (s *Store) UpdateRider(_ context.Context, userID string, request rider.UpdateRequest) (*rider.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.riders[userID]
	if item == nil || item.Archived {
		return nil, domain.ErrNotFound
	}
	item.DisplayName, item.VehicleType = request.DisplayName, request.VehicleType
	item.Skills, item.ServiceArea = append([]string(nil), request.Skills...), request.ServiceArea
	item.MaxDistanceMeters, item.MaxConcurrentTasks = request.MaxDistanceMeters, request.MaxConcurrentTasks
	item.UpdatedAt = time.Now().UTC()
	return cloneRider(item), nil
}

func (s *Store) ArchiveRider(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.riders[userID]
	if item == nil || item.Archived {
		return domain.ErrNotFound
	}
	item.Archived, item.Status, item.UpdatedAt = true, "SUSPENDED", time.Now().UTC()
	return nil
}

func (s *Store) SetRiderStatus(_ context.Context, userID, status string) (*rider.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.riders[userID]
	if item == nil || item.Archived {
		return nil, domain.ErrNotFound
	}
	if item.Status == "SUSPENDED" {
		return nil, domain.ErrConflict
	}
	item.Status, item.UpdatedAt = status, time.Now().UTC()
	return cloneRider(item), nil
}

func (s *Store) FindIdempotent(_ context.Context, owner, key, hash string) (*task.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.idempotency[owner+":"+key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if record.hash != hash {
		return nil, fmt.Errorf("%w: idempotency key reused with different task", domain.ErrConflict)
	}
	return clone(s.tasks[record.id]), nil
}

func (s *Store) Create(_ context.Context, request task.CreateRequest, createdBy string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.IdempotencyKey != "" {
		if existing, ok := s.idempotency[createdBy+":"+request.IdempotencyKey]; ok {
			if existing.hash != request.IdempotencyHash {
				return nil, fmt.Errorf("%w: idempotency key reused with different task", domain.ErrConflict)
			}
			return clone(s.tasks[existing.id]), nil
		}
	}
	s.nextID++
	now := time.Now().UTC()
	id := fmt.Sprintf("task-%04d", s.nextID)
	item := &task.Task{QuotedAmountFen: copyAmount(request.QuotedAmountFen), DestinationPOIID: request.DestinationPOIID, PickupStops: cloneStops(request.PickupStops), DeliveryAt: copyTime(request.DeliveryAt), PickupLocation: copyPoint(request.PickupLocation), DestinationLocation: copyPoint(request.DestinationLocation), ID: id, Title: request.Title, Description: request.Description, ContactName: request.ContactName, ContactPhone: request.ContactPhone, HandoverNote: request.HandoverNote, Type: request.Type, Priority: request.Priority, Pickup: request.Pickup, Destination: request.Destination, Status: task.StatusOpen, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now, Events: []task.TaskEvent{{To: task.StatusOpen, ActorID: createdBy, CreatedAt: now}}}
	s.tasks[id] = item
	if request.IdempotencyKey != "" {
		s.idempotency[createdBy+":"+request.IdempotencyKey] = idempotentTask{id, request.IdempotencyHash}
	}
	return clone(item), nil
}

func (s *Store) List(_ context.Context, status string, page repository.Page) ([]*task.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*task.Task, 0, len(s.tasks))
	for _, item := range s.tasks {
		if status == "" || item.Status == status {
			result = append(result, clone(item))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return pageTasks(result, page), nil
}

func (s *Store) ListByCreator(_ context.Context, creatorID, status string, page repository.Page) ([]*task.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*task.Task, 0)
	for _, item := range s.tasks {
		if item.CreatedBy == creatorID && (status == "" || item.Status == status) {
			items = append(items, clone(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return pageTasks(items, page), nil
}

func (s *Store) ListForRider(_ context.Context, riderID, status string, page repository.Page) ([]*task.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*task.Task, 0)
	for _, item := range s.tasks {
		visible := item.WorkerID == riderID || (item.Status == task.StatusOpen && (status == "" || status == task.StatusOpen))
		if visible && (status == "" || item.Status == status) {
			items = append(items, clone(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return pageTasks(items, page), nil
}

func (s *Store) Get(_ context.Context, id string) (*task.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.tasks[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	return clone(item), nil
}

func (s *Store) Update(_ context.Context, id string, request task.UpdateRequest) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.tasks[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status != task.StatusOpen {
		return nil, domain.ErrConflict
	}
	item.PickupStops, item.DeliveryAt = cloneStops(request.PickupStops), copyTime(request.DeliveryAt)
	item.QuotedAmountFen = copyAmount(request.QuotedAmountFen)
	item.DestinationPOIID = request.DestinationPOIID
	item.PickupLocation, item.DestinationLocation = copyPoint(request.PickupLocation), copyPoint(request.DestinationLocation)
	item.Title, item.Description, item.Type = request.Title, request.Description, request.Type
	item.ContactName, item.ContactPhone, item.HandoverNote = request.ContactName, request.ContactPhone, request.HandoverNote
	item.Priority, item.Pickup, item.Destination = request.Priority, request.Pickup, request.Destination
	item.UpdatedAt = time.Now().UTC()
	return clone(item), nil
}

func (s *Store) Claim(_ context.Context, id, riderID string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.tasks[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status != task.StatusOpen {
		return nil, fmt.Errorf("%w: task is no longer open", domain.ErrConflict)
	}
	if strings.TrimSpace(riderID) == "" {
		return nil, errors.New("rider id is required")
	}
	if profile := s.riders[riderID]; profile != nil {
		if profile.Archived {
			return nil, domain.ErrForbidden
		}
		if profile.Status != "AVAILABLE" {
			return nil, fmt.Errorf("%w: 请先把接单状态切换为上线", domain.ErrConflict)
		}
		active := 0
		for _, candidate := range s.tasks {
			if candidate.WorkerID == riderID && (candidate.Status == task.StatusClaimed || candidate.Status == task.StatusArrived || candidate.Status == task.StatusProcessing) {
				active++
			}
		}
		if profile.MaxConcurrentTasks > 0 && active >= profile.MaxConcurrentTasks {
			return nil, fmt.Errorf("%w: 同时进行的任务已达上限（%d 单）", domain.ErrConflict, profile.MaxConcurrentTasks)
		}
	}
	now := time.Now().UTC()
	item.WorkerID = riderID
	item.Status = task.StatusClaimed
	item.UpdatedAt = now
	item.Events = append(item.Events, task.TaskEvent{From: task.StatusOpen, To: task.StatusClaimed, ActorID: riderID, CreatedAt: now})
	return clone(item), nil
}

func (s *Store) UpdateStatus(_ context.Context, id, next, actorID string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.tasks[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if !task.ValidTransition(item.Status, next) {
		return nil, fmt.Errorf("%w: invalid status transition %s -> %s", domain.ErrConflict, item.Status, next)
	}
	if next == task.StatusCompleted && !task.PickupsComplete(item.PickupStops) {
		return nil, fmt.Errorf("%w: 请先完成所有取件点", domain.ErrConflict)
	}
	previous, now := item.Status, time.Now().UTC()
	item.Status = next
	item.UpdatedAt = now
	item.Events = append(item.Events, task.TaskEvent{From: previous, To: next, ActorID: actorID, CreatedAt: now})
	if next == task.StatusCompleted && item.WorkerID != "" {
		if profile := s.riders[item.WorkerID]; profile != nil {
			profile.CompletedTasks++
			profile.UpdatedAt = now
		}
	}
	return clone(item), nil
}

func (s *Store) AdvancePickup(_ context.Context, id, actor string, index int, next string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.tasks[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.WorkerID != actor {
		return nil, domain.ErrForbidden
	}
	if item.Status != task.StatusClaimed && item.Status != task.StatusArrived && item.Status != task.StatusProcessing {
		return nil, domain.ErrConflict
	}
	if err := task.AdvancePickup(item.PickupStops, index, next); err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrConflict, err)
	}
	now := time.Now().UTC()
	item.UpdatedAt = now
	item.Events = append(item.Events, task.TaskEvent{To: fmt.Sprintf("PICKUP_%d_%s", index+1, next), ActorID: actor, CreatedAt: now})
	return clone(item), nil
}

func (s *Store) CountActiveForRider(_ context.Context, riderID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, item := range s.tasks {
		if item.WorkerID == riderID && (item.Status == task.StatusClaimed || item.Status == task.StatusArrived || item.Status == task.StatusProcessing) {
			count++
		}
	}
	return count, nil
}

func (s *Store) CreateIncident(_ context.Context, incident task.Incident) (*task.Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.tasks[incident.TaskID]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if s.incidents == nil {
		s.incidents = map[string]*task.Incident{}
	}
	if s.incidents[incident.ID] != nil {
		return nil, domain.ErrConflict
	}
	incident.Status = task.IncidentOpen
	incident.CreatedAt = time.Now().UTC()
	stored := incident
	stored.ImageURLs = append([]string(nil), incident.ImageURLs...)
	s.incidents[incident.ID] = &stored
	item.Incidents = append(item.Incidents, stored)
	return &stored, nil
}

func (s *Store) ListIncidents(_ context.Context, taskID string) ([]task.Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.tasks[taskID]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	return append([]task.Incident(nil), item.Incidents...), nil
}

func (s *Store) ListIncidentsByStatus(_ context.Context, status string, page repository.Page) ([]task.Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]task.Incident, 0, len(s.incidents))
	for _, incident := range s.incidents {
		if incident == nil || (status != "" && incident.Status != status) {
			continue
		}
		all = append(all, *incident)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	if page.Offset >= len(all) {
		return []task.Incident{}, nil
	}
	end := page.Offset + page.Limit
	if end > len(all) {
		end = len(all)
	}
	return append([]task.Incident(nil), all[page.Offset:end]...), nil
}

func (s *Store) ResolveIncident(_ context.Context, taskID, incidentID, resolverID, resolution string, at time.Time) (*task.Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.tasks[taskID]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	incident := s.incidents[incidentID]
	if incident == nil || incident.TaskID != taskID {
		return nil, domain.ErrNotFound
	}
	if incident.Status != task.IncidentOpen {
		return nil, domain.ErrConflict
	}
	incident.Status, incident.Resolution, incident.ResolvedBy, incident.ResolvedAt = task.IncidentResolved, resolution, resolverID, &at
	for i := range item.Incidents {
		if item.Incidents[i].ID == incidentID {
			item.Incidents[i] = *incident
		}
	}
	return incident, nil
}

func clone(item *task.Task) *task.Task {
	copy := *item
	copy.QuotedAmountFen = copyAmount(item.QuotedAmountFen)
	copy.Events = append([]task.TaskEvent(nil), item.Events...)
	copy.Incidents = append([]task.Incident(nil), item.Incidents...)
	copy.PickupStops, copy.DeliveryAt = cloneStops(item.PickupStops), copyTime(item.DeliveryAt)
	copy.PickupLocation, copy.DestinationLocation = copyPoint(item.PickupLocation), copyPoint(item.DestinationLocation)
	return &copy
}

func copyAmount(value *int64) *int64 {
	if value == nil {
		return nil
	}
	amount := *value
	return &amount
}

func cloneMerchant(item *merchant.Profile) *merchant.Profile {
	copy := *item
	copy.ServiceTypes = append([]string(nil), item.ServiceTypes...)
	return &copy
}

func cloneRider(item *rider.Profile) *rider.Profile {
	copy := *item
	copy.Skills = append([]string(nil), item.Skills...)
	return &copy
}

func pageSlice[T any](items []T, page repository.Page) []T {
	if page.Limit <= 0 || page.Limit > 200 {
		page.Limit = 50
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	if page.Offset >= len(items) {
		return make([]T, 0)
	}
	end := page.Offset + page.Limit
	if end > len(items) {
		end = len(items)
	}
	return items[page.Offset:end]
}

func pageTasks(items []*task.Task, page repository.Page) []*task.Task { return pageSlice(items, page) }
func pageUsers(items []*user.User, page repository.Page) []*user.User { return pageSlice(items, page) }
func pageMerchants(items []*merchant.Profile, page repository.Page) []*merchant.Profile {
	return pageSlice(items, page)
}
func pageRiders(items []*rider.Profile, page repository.Page) []*rider.Profile {
	return pageSlice(items, page)
}

func copyPoint(p *location.Point) *location.Point {
	if p == nil {
		return nil
	}
	copy := *p
	return &copy
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneStops(stops []task.PickupStop) []task.PickupStop {
	if stops == nil {
		return nil
	}
	result := make([]task.PickupStop, len(stops))
	for i, stop := range stops {
		result[i] = stop
		result[i].Location = copyPoint(stop.Location)
		result[i].PickupAt = copyTime(stop.PickupAt)
	}
	return result
}
