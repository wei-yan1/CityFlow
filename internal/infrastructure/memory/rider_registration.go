package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/user"
)

func cloneRiderRegistration(item *rider.Registration) *rider.Registration {
	copy := *item
	copy.Skills = append([]string(nil), item.Skills...)
	copy.ReviewedAt = copyTime(item.ReviewedAt)
	return &copy
}

func (s *Store) CreateRiderRegistration(_ context.Context, item rider.Registration) (*rider.Registration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.Email == "" || item.PaymentReference == "" || item.PaidAmountFen < rider.RegistrationMinimumFen {
		return nil, fmt.Errorf("%w: invalid rider registration", domain.ErrInvalidRequest)
	}
	if userID, exists := s.byEmail[item.Email]; exists && userID != "" {
		return nil, fmt.Errorf("%w: email is already registered", domain.ErrConflict)
	}
	if item.Phone != "" {
		for _, existing := range s.users {
			if existing.Phone == item.Phone {
				return nil, fmt.Errorf("%w: phone is already registered", domain.ErrConflict)
			}
		}
	}
	for _, existing := range s.riderRegistrations {
		if existing.PaymentReference == item.PaymentReference {
			return nil, fmt.Errorf("%w: payment reference was already submitted", domain.ErrConflict)
		}
		if existing.Status == rider.RegistrationPending && (existing.Email == item.Email || (item.Phone != "" && existing.Phone == item.Phone)) {
			return nil, fmt.Errorf("%w: a pending rider registration already exists", domain.ErrConflict)
		}
	}
	now := time.Now().UTC()
	item.ID = fmt.Sprintf("registration-%04d", len(s.riderRegistrations)+1)
	if item.Status == "" {
		item.Status = rider.RegistrationPending
	}
	item.CreatedAt, item.UpdatedAt = now, now
	s.riderRegistrations[item.ID] = cloneRiderRegistration(&item)
	return cloneRiderRegistration(&item), nil
}

func (s *Store) RiderRegistrationExists(_ context.Context, email, phone string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if userID, exists := s.byEmail[email]; exists && userID != "" {
		return true, nil
	}
	if phone != "" {
		for _, existing := range s.users {
			if existing.Phone == phone {
				return true, nil
			}
		}
	}
	for _, existing := range s.riderRegistrations {
		if existing.Status != rider.RegistrationPending {
			continue
		}
		if existing.Email == email || (phone != "" && existing.Phone == phone) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) ListRiderRegistrations(_ context.Context, status string, page repository.Page) ([]*rider.Registration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*rider.Registration, 0, len(s.riderRegistrations))
	for _, item := range s.riderRegistrations {
		if status == "" || item.Status == status {
			items = append(items, cloneRiderRegistration(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt) || (items[i].CreatedAt.Equal(items[j].CreatedAt) && items[i].ID > items[j].ID)
	})
	return pageSlice(items, page), nil
}

func (s *Store) GetRiderRegistration(_ context.Context, id string) (*rider.Registration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.riderRegistrations[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	return cloneRiderRegistration(item), nil
}

func (s *Store) ApproveRiderRegistration(_ context.Context, id, adminID, note string) (*rider.Registration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.riderRegistrations[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status != rider.RegistrationPending {
		return nil, fmt.Errorf("%w: rider registration is already %s", domain.ErrConflict, item.Status)
	}
	if item.PaidAmountFen < rider.RegistrationMinimumFen {
		return nil, fmt.Errorf("%w: rider registration payment is below the minimum", domain.ErrInvalidRequest)
	}
	if userID, exists := s.byEmail[item.Email]; exists && userID != "" {
		return nil, fmt.Errorf("%w: email is already registered", domain.ErrConflict)
	}
	if item.Phone != "" {
		for _, existing := range s.users {
			if existing.Phone == item.Phone {
				return nil, fmt.Errorf("%w: phone is already registered", domain.ErrConflict)
			}
		}
	}
	s.nextID++
	now := time.Now().UTC()
	userID := fmt.Sprintf("user-%04d", s.nextID)
	account := &user.User{
		ID: userID, Email: item.Email, Name: item.DisplayName, Phone: item.Phone,
		Role: auth.RoleRider, Status: "ACTIVE", PasswordHash: item.PasswordHash, CreatedAt: now, UpdatedAt: now,
	}
	s.users[userID], s.byEmail[item.Email] = account, userID
	s.nextID++
	riderID := fmt.Sprintf("rider-%04d", s.nextID)
	profile := &rider.Profile{
		ID: riderID, UserID: userID, DisplayName: item.DisplayName, VehicleType: item.VehicleType,
		Skills: append([]string(nil), item.Skills...), ServiceArea: item.ServiceArea,
		MaxDistanceMeters: item.MaxDistanceMeters, MaxConcurrentTasks: item.MaxConcurrentTasks,
		Status: "OFFLINE", Rating: 5, CreatedAt: now, UpdatedAt: now,
	}
	s.riders[userID] = profile
	item.Status, item.UserID = rider.RegistrationApproved, userID
	item.ReviewNote, item.ReviewedBy, item.ReviewedAt, item.UpdatedAt = note, adminID, copyTime(&now), now
	return cloneRiderRegistration(item), nil
}

func (s *Store) RejectRiderRegistration(_ context.Context, id, adminID, note string) (*rider.Registration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.riderRegistrations[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status != rider.RegistrationPending {
		return nil, fmt.Errorf("%w: rider registration is already %s", domain.ErrConflict, item.Status)
	}
	now := time.Now().UTC()
	item.Status, item.ReviewNote, item.ReviewedBy, item.ReviewedAt, item.UpdatedAt = rider.RegistrationRejected, note, adminID, copyTime(&now), now
	return cloneRiderRegistration(item), nil
}
