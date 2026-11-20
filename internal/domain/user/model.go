package user

import "time"

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Phone        string    `json:"phone,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	PasswordHash string    `json:"-"`
}

type CreateRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
}

type UpdateRequest struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	AvatarURL string `json:"avatar_url"`
}

type PasswordReset struct {
	TokenHash string    `json:"-"`
	UserID    string    `json:"-"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type PasswordResetResult struct {
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	DevToken  string    `json:"dev_token,omitempty"`
	Message   string    `json:"message"`
}

type PasswordResetRequest struct {
	Email string `json:"email"`
}

type PasswordResetConfirm struct {
	Email       string `json:"email"`
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
}
