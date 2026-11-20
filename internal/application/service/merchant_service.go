package service

import (
	"context"
	"fmt"
	"strings"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/merchant"
)

type MerchantService struct {
	profiles repository.MerchantRepository
	accounts accountReader
}

func NewMerchantService(profiles repository.MerchantRepository, accounts accountReader) *MerchantService {
	return &MerchantService{profiles: profiles, accounts: accounts}
}
func (s *MerchantService) Profile(ctx context.Context, userID string) (*merchant.Profile, error) {
	return s.profiles.MerchantForUser(ctx, userID)
}

func (s *MerchantService) List(ctx context.Context, status string, page repository.Page) ([]*merchant.Profile, error) {
	return s.profiles.ListMerchants(ctx, strings.ToUpper(strings.TrimSpace(status)), page)
}

func (s *MerchantService) Create(ctx context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	account, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if account.Role != auth.RoleMerchant {
		return nil, domain.ErrForbidden
	}
	if account.Status != "ACTIVE" {
		return nil, domain.ErrUnauthorized
	}
	request, err = normalizeMerchantRequest(request)
	if err != nil {
		return nil, err
	}
	return s.profiles.CreateMerchant(ctx, userID, request)
}

func (s *MerchantService) Update(ctx context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	account, err := s.accounts.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if account.Role != auth.RoleMerchant {
		return nil, domain.ErrForbidden
	}
	if account.Status != "ACTIVE" {
		return nil, domain.ErrUnauthorized
	}
	request, err = normalizeMerchantRequest(request)
	if err != nil {
		return nil, err
	}
	return s.profiles.UpdateMerchant(ctx, userID, request)
}

func (s *MerchantService) Archive(ctx context.Context, userID string) error {
	return s.profiles.ArchiveMerchant(ctx, userID)
}

func normalizeMerchantRequest(request merchant.UpdateRequest) (merchant.UpdateRequest, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	request.Phone = strings.TrimSpace(request.Phone)
	request.Address = strings.TrimSpace(request.Address)
	request.BusinessStatus = strings.ToUpper(strings.TrimSpace(request.BusinessStatus))
	request.AMAPPOIID = strings.TrimSpace(request.AMAPPOIID)
	if len(request.AMAPPOIID) > 128 {
		return request, fmt.Errorf("%w: 高德 POI ID 最多 128 个字符", domain.ErrInvalidRequest)
	}
	if request.Name == "" || request.Address == "" || request.Latitude < -90 || request.Latitude > 90 || request.Longitude < -180 || request.Longitude > 180 || request.CapacityPerHour < 0 {
		return request, fmt.Errorf("%w: name, address, valid coordinates and non-negative capacity are required", domain.ErrInvalidRequest)
	}
	switch request.BusinessStatus {
	case "OPEN", "CLOSED", "PAUSED":
	default:
		return request, fmt.Errorf("%w: business_status must be OPEN, CLOSED or PAUSED", domain.ErrInvalidRequest)
	}
	request.ServiceTypes = normalizeTypes(request.ServiceTypes)
	if len(request.ServiceTypes) == 0 {
		return request, fmt.Errorf("%w: at least one service type is required", domain.ErrInvalidRequest)
	}
	return request, nil
}

func normalizeTypes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
