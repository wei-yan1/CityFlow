package authservice

import (
	"context"
	"strings"
	"time"

	"cityflow/app/identity/domain/auth"
	"cityflow/app/identity/domain/user"
	"cityflow/internal/security"
)

// AccountStore is the identity-owned account surface AuthService needs.
// Keeping it local to this package avoids pulling every repository interface
// (and therefore every other service's domain) into the auth build.
type AccountStore interface {
	Authenticate(context.Context, string, string) (*user.User, error)
	GetUser(context.Context, string) (*user.User, error)
	IsActive(context.Context, string) (bool, error)
}

type AuthService struct {
	accounts AccountStore
	secret   []byte
}

func NewAuthService(accounts AccountStore, secret []byte) *AuthService {
	return &AuthService{accounts: accounts, secret: append([]byte(nil), secret...)}
}

func (s *AuthService) Login(ctx context.Context, request auth.LoginRequest) (*auth.LoginResponse, error) {
	account, err := s.accounts.Authenticate(ctx, strings.TrimSpace(request.Email), request.Password)
	if err != nil {
		return nil, err
	}
	token, expires, err := security.SignJWT(account.ID, account.Email, account.Role, s.secret, time.Now().UTC())
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
