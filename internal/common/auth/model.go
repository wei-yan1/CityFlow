// Package auth carries the cross-service authentication contract: role names
// and the JWT claim shape. Identity owns accounts; every service reads these
// stable values from here rather than importing Identity's domain package.
package auth

import "time"

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
