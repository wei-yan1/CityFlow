package merchantservice

import (
	"context"
	"fmt"
	"strings"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/app/discovery/repository"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
)

// Account is Identity's account projection as Discovery needs it. Discovery
// reads the fields over RPC and does not import Identity's domain package.
type Account struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// AccountReader is the identity fact Discovery needs. It is declared by the
// consumer so Discovery does not depend on Identity's repository package.
type AccountReader interface {
	GetAccount(context.Context, string) (*Account, error)
}

// ProfileCache invalidates the hot-list merchant detail cache when a profile
// changes. It is optional: Discovery still works when Redis is not configured.
type ProfileCache interface {
	InvalidateMerchantProfile(context.Context, string) error
}

type MerchantService struct {
	profiles     repository.MerchantRepository
	accounts     AccountReader
	profileCache ProfileCache
}

func NewMerchantService(profiles repository.MerchantRepository, accounts AccountReader) *MerchantService {
	return &MerchantService{profiles: profiles, accounts: accounts}
}

func (s *MerchantService) SetProfileCache(cache ProfileCache) {
	s.profileCache = cache
}
func (s *MerchantService) Profile(ctx context.Context, userID string) (*merchant.Profile, error) {
	return s.profiles.MerchantForUser(ctx, userID)
}

func (s *MerchantService) List(ctx context.Context, status string, page repository.Page) ([]*merchant.Profile, error) {
	return s.profiles.ListMerchants(ctx, strings.ToUpper(strings.TrimSpace(status)), page)
}

func (s *MerchantService) Create(ctx context.Context, userID string, request merchant.UpdateRequest) (*merchant.Profile, error) {
	account, err := s.accounts.GetAccount(ctx, userID)
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
	account, err := s.accounts.GetAccount(ctx, userID)
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
	updated, err := s.profiles.UpdateMerchant(ctx, userID, request)
	if err != nil {
		return nil, err
	}
	if s.profileCache != nil {
		_ = s.profileCache.InvalidateMerchantProfile(ctx, updated.ID)
	}
	return updated, nil
}

func (s *MerchantService) Archive(ctx context.Context, userID string) error {
	profile, err := s.profiles.MerchantForUser(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.profiles.ArchiveMerchant(ctx, userID); err != nil {
		return err
	}
	if s.profileCache != nil {
		_ = s.profileCache.InvalidateMerchantProfile(ctx, profile.ID)
	}
	return nil
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
	if request.Name == "" || request.Address == "" || request.Latitude < -90 || request.Latitude > 90 || request.Longitude < -180 || request.Longitude > 180 || request.CapacityPerHour < 1 {
		return request, fmt.Errorf("%w: name, address, valid coordinates and capacity of at least 1 per hour are required", domain.ErrInvalidRequest)
	}
	switch request.BusinessStatus {
	case "OPEN", "CLOSED", "PAUSED":
	default:
		return request, fmt.Errorf("%w: business_status must be OPEN, CLOSED or PAUSED", domain.ErrInvalidRequest)
	}
	request.ServiceTypes = merchant.NormalizeServiceTypes(request.ServiceTypes)
	if len(request.ServiceTypes) != 1 {
		return request, fmt.Errorf("%w: service_types must contain exactly one of FOOD, SUPPLIES or OTHER", domain.ErrInvalidRequest)
	}
	return request, nil
}
