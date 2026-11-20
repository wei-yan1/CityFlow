package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/security"
)

// RefreshStore manages opaque, single-use refresh credentials. Redis failures
// are returned to callers; an outage never grants a new session.
type RefreshStore interface {
	CreateRefreshSession(context.Context, string, string, time.Duration) error
	GetRefreshSession(context.Context, string) (string, error)
	RotateRefreshSession(context.Context, string, string, time.Duration) (string, error)
	RevokeRefreshSession(context.Context, string) error
}

const refreshLifetime = 7 * 24 * time.Hour

func newRefreshToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

// RefreshSession deliberately returns both credentials only after the old
// refresh token is atomically consumed and the account is rechecked.
func (s *AuthService) RefreshSession(ctx context.Context, store RefreshStore, previous string) (*auth.LoginResponse, string, error) {
	if store == nil {
		return nil, "", domain.ErrUnavailable
	}
	if previous == "" {
		return nil, "", domain.ErrUnauthorized
	}
	id, err := store.GetRefreshSession(ctx, previous)
	if err != nil {
		return nil, "", err
	}
	account, err := s.accounts.GetUser(ctx, id)
	if err != nil {
		return nil, "", err
	}
	active, err := s.accounts.IsActive(ctx, id)
	if err != nil || !active {
		if err != nil {
			return nil, "", domain.ErrUnavailable
		}
		_ = store.RevokeRefreshSession(ctx, previous)
		return nil, "", domain.ErrUnauthorized
	}
	next, err := newRefreshToken()
	if err != nil {
		return nil, "", err
	}
	rotatedID, err := store.RotateRefreshSession(ctx, previous, next, refreshLifetime)
	if err != nil {
		return nil, "", err
	}
	if rotatedID != id {
		_ = store.RevokeRefreshSession(ctx, next)
		return nil, "", domain.ErrUnauthorized
	}
	token, expires, err := security.SignJWT(account, s.secret, time.Now().UTC())
	if err != nil {
		_ = store.RevokeRefreshSession(ctx, next)
		return nil, "", err
	}
	return &auth.LoginResponse{AccessToken: token, TokenType: "Bearer", ExpiresAt: expires, User: account}, next, nil
}

func (s *AuthService) StartRefreshSession(ctx context.Context, store RefreshStore, id string) (string, error) {
	if store == nil {
		return "", errors.New("refresh session unavailable")
	}
	token, err := newRefreshToken()
	if err != nil {
		return "", err
	}
	if err = store.CreateRefreshSession(ctx, token, id, refreshLifetime); err != nil {
		return "", err
	}
	return token, nil
}
