package service

import (
	"context"
	"strings"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/user"
	"cityflow/internal/security"
)

type AuthService struct {
	accounts  repository.AuthRepository
	merchants repository.MerchantRepository
	riders    repository.RiderRepository
	secret    []byte
}

func NewAuthService(accounts repository.AuthRepository, merchants repository.MerchantRepository, riders repository.RiderRepository, secret []byte) *AuthService {
	return &AuthService{accounts: accounts, merchants: merchants, riders: riders, secret: append([]byte(nil), secret...)}
}

func (s *AuthService) Login(ctx context.Context, request auth.LoginRequest) (*auth.LoginResponse, error) {
	account, err := s.accounts.Authenticate(ctx, strings.TrimSpace(request.Email), request.Password)
	if err != nil {
		return nil, err
	}
	token, expires, err := security.SignJWT(account, s.secret, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &auth.LoginResponse{AccessToken: token, TokenType: "Bearer", ExpiresAt: expires, User: account}, nil
}

func (s *AuthService) Me(ctx context.Context, id string) (*user.User, error) {
	return s.accounts.GetUser(ctx, id)
}

func (s *AuthService) IsActive(ctx context.Context, id string) (bool, error) {
	return s.accounts.IsActive(ctx, id)
}

func (s *AuthService) MerchantMe(ctx context.Context, id string) (*merchant.Profile, error) {
	if s.merchants == nil {
		return nil, domain.ErrUnavailable
	}
	return s.merchants.MerchantForUser(ctx, id)
}

func (s *AuthService) RiderMe(ctx context.Context, id string) (*rider.Profile, error) {
	if s.riders == nil {
		return nil, domain.ErrUnavailable
	}
	return s.riders.RiderForUser(ctx, id)
}
