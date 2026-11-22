package auth

import (
	"time"

	"cityflow/app/identity/domain/user"
	commonauth "cityflow/internal/common/auth"
)

const (
	RoleUser     = commonauth.RoleUser
	RoleMerchant = commonauth.RoleMerchant
	RoleRider    = commonauth.RoleRider
	RoleAdmin    = commonauth.RoleAdmin
)

const AccessTokenLifetime = commonauth.AccessTokenLifetime

// Claims is the shared JWT shape. Platform code depends on internal/common/auth
// directly; Identity re-exports it only for its own application layer.
type Claims = commonauth.Claims

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken string     `json:"access_token"`
	TokenType   string     `json:"token_type"`
	ExpiresAt   time.Time  `json:"expires_at"`
	User        *user.User `json:"user"`
}
