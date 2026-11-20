package service

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/rider"
	"cityflow/internal/security"
)

type RiderRegistrationService struct {
	registrations repository.RiderRegistrationRepository
}

func NewRiderRegistrationService(registrations repository.RiderRegistrationRepository) *RiderRegistrationService {
	return &RiderRegistrationService{registrations: registrations}
}

// Submit records the applicant's paid claim. It deliberately does not create a
// login account: a personal collection QR has no provider callback, so an
// administrator must verify the receipt before the rider is activated.
func (s *RiderRegistrationService) Submit(ctx context.Context, request rider.RegistrationRequest) (*rider.Registration, error) {
	request, err := normalizeRegistrationRequest(request)
	if err != nil {
		return nil, err
	}
	hash, err := security.HashPassword(request.Password)
	if err != nil {
		return nil, fmt.Errorf("hash rider password: %w", err)
	}
	exists, err := s.registrations.RiderRegistrationExists(ctx, request.Email, request.Phone)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: email, phone or payment reference is already in use", domain.ErrConflict)
	}
	return s.registrations.CreateRiderRegistration(ctx, rider.Registration{
		Email:              request.Email,
		Phone:              request.Phone,
		DisplayName:        request.DisplayName,
		VehicleType:        request.VehicleType,
		Skills:             request.Skills,
		ServiceArea:        request.ServiceArea,
		MaxDistanceMeters:  request.MaxDistanceMeters,
		MaxConcurrentTasks: request.MaxConcurrentTasks,
		PaidAmountFen:      request.PaidAmountFen,
		PaymentReference:   request.PaymentReference,
		PaymentNote:        request.PaymentNote,
		Status:             rider.RegistrationPending,
		PasswordHash:       hash,
	})
}

func (s *RiderRegistrationService) List(ctx context.Context, status string, page repository.Page) ([]*rider.Registration, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	switch status {
	case "", rider.RegistrationPending, rider.RegistrationApproved, rider.RegistrationRejected:
	default:
		return nil, fmt.Errorf("%w: unsupported registration status", domain.ErrInvalidRequest)
	}
	return s.registrations.ListRiderRegistrations(ctx, status, page)
}

func (s *RiderRegistrationService) Approve(ctx context.Context, id, adminID, note string) (*rider.Registration, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(adminID) == "" {
		return nil, fmt.Errorf("%w: registration and administrator are required", domain.ErrInvalidRequest)
	}
	return s.registrations.ApproveRiderRegistration(ctx, id, adminID, strings.TrimSpace(note))
}

func (s *RiderRegistrationService) Reject(ctx context.Context, id, adminID, note string) (*rider.Registration, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(adminID) == "" {
		return nil, fmt.Errorf("%w: registration and administrator are required", domain.ErrInvalidRequest)
	}
	note = strings.TrimSpace(note)
	if len(note) > 255 {
		return nil, fmt.Errorf("%w: review note is too long", domain.ErrInvalidRequest)
	}
	return s.registrations.RejectRiderRegistration(ctx, id, adminID, note)
}

func normalizeRegistrationRequest(request rider.RegistrationRequest) (rider.RegistrationRequest, error) {
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	if parsed, err := mail.ParseAddress(request.Email); err != nil || parsed.Address != request.Email {
		return request, fmt.Errorf("%w: a valid email is required", domain.ErrInvalidRequest)
	}
	request.Password = strings.TrimSpace(request.Password)
	if len(request.Password) < 8 {
		return request, fmt.Errorf("%w: password must be at least 8 characters", domain.ErrInvalidRequest)
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	if request.DisplayName == "" || len(request.DisplayName) > 80 {
		return request, fmt.Errorf("%w: display_name is required and must be at most 80 characters", domain.ErrInvalidRequest)
	}
	request.Phone = strings.TrimSpace(request.Phone)
	if len(request.Phone) > 32 {
		return request, fmt.Errorf("%w: phone is too long", domain.ErrInvalidRequest)
	}
	request.VehicleType = strings.ToUpper(strings.TrimSpace(request.VehicleType))
	switch request.VehicleType {
	case "WALK", "BICYCLE", "E_BIKE", "CAR", "OTHER":
	default:
		return request, fmt.Errorf("%w: unsupported vehicle_type", domain.ErrInvalidRequest)
	}
	request.Skills = normalizeTypes(request.Skills)
	if len(request.Skills) == 0 {
		return request, fmt.Errorf("%w: at least one service skill is required", domain.ErrInvalidRequest)
	}
	request.ServiceArea = strings.TrimSpace(request.ServiceArea)
	if len(request.ServiceArea) > 160 {
		return request, fmt.Errorf("%w: service_area is too long", domain.ErrInvalidRequest)
	}
	if request.MaxDistanceMeters <= 0 || request.MaxDistanceMeters > 100000 {
		return request, fmt.Errorf("%w: max_distance_meters must be between 1 and 100000", domain.ErrInvalidRequest)
	}
	if request.MaxConcurrentTasks <= 0 || request.MaxConcurrentTasks > 20 {
		return request, fmt.Errorf("%w: max_concurrent_tasks must be between 1 and 20", domain.ErrInvalidRequest)
	}
	if request.PaidAmountFen < rider.RegistrationMinimumFen {
		return request, fmt.Errorf("%w: rider registration requires at least 0.01 CNY", domain.ErrInvalidRequest)
	}
	request.PaymentReference = strings.TrimSpace(request.PaymentReference)
	if request.PaymentReference == "" || len(request.PaymentReference) > 128 {
		return request, fmt.Errorf("%w: payment_reference is required and must be at most 128 characters", domain.ErrInvalidRequest)
	}
	request.PaymentNote = strings.TrimSpace(request.PaymentNote)
	if len(request.PaymentNote) > 255 {
		return request, fmt.Errorf("%w: payment_note is too long", domain.ErrInvalidRequest)
	}
	return request, nil
}
