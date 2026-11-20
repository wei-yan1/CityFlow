// Package authn contains transport-level bearer authentication used by the
// go-zero Gateway, Identity and Task services.
package authn

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/security"
)

type AccountStatus interface {
	IsActive(context.Context, string) (bool, error)
}

// BearerToken extracts a Bearer access token without accepting tokens from
// query strings or cookies for ordinary REST calls.
func BearerToken(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	return token, token != ""
}

// FromRequest verifies the access token and the account's current status.
func FromRequest(ctx context.Context, r *http.Request, secret []byte, accounts AccountStatus) (*auth.Claims, error) {
	token, ok := BearerToken(r)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	claims, err := security.VerifyJWT(token, secret, time.Now().UTC())
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	active, err := accounts.IsActive(ctx, claims.Subject)
	if err != nil {
		return nil, errors.Join(domain.ErrUnavailable, err)
	}
	if !active {
		return nil, domain.ErrUnauthorized
	}
	return claims, nil
}
