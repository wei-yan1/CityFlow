package auth

import (
	"time"

	"cityflow/internal/domain/user"
)

const (
	RoleUser     = "USER"
	RoleMerchant = "MERCHANT"
	RoleRider    = "RIDER"
	RoleAdmin    = "ADMIN"
)

const AccessTokenLifetime = 24 * time.Hour

type Claims struct {
	Issuer   string `json:"iss"`
	Subject  string `json:"sub"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

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
