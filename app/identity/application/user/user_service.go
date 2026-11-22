package userservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"cityflow/app/identity/domain/user"
	"cityflow/app/identity/repository"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
	"cityflow/internal/security"
)

type UserService struct {
	mapInvalidation  func()
	accounts         repository.AuthRepository
	users            repository.UserRepository
	exposeResetToken bool
}

func (s *UserService) WithMapInvalidation(fn func()) *UserService { s.mapInvalidation = fn; return s }

func (s *UserService) WithResetTokenExposure(expose bool) *UserService {
	s.exposeResetToken = expose
	return s
}

func NewUserService(accounts repository.AuthRepository, users repository.UserRepository) *UserService {
	return &UserService{accounts: accounts, users: users}
}
func (s *UserService) Profile(ctx context.Context, userID string) (*user.User, error) {
	return s.accounts.GetUser(ctx, userID)
}

var (
	accountEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	accountPhonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
)

func (s *UserService) CreateAccount(ctx context.Context, request user.CreateRequest) (*user.User, error) {
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	request.Name = strings.TrimSpace(request.Name)
	request.Phone = strings.TrimSpace(request.Phone)
	request.Role = strings.ToUpper(strings.TrimSpace(request.Role))
	address, err := mail.ParseAddress(request.Email)
	if err != nil || request.Name == "" || len(request.Password) < 8 || !accountEmailPattern.MatchString(request.Email) || (request.Phone != "" && !accountPhonePattern.MatchString(request.Phone)) {
		return nil, fmt.Errorf("%w: 请填写有效邮箱、姓名和至少 8 位密码；手机号如填写需为 11 位中国大陆手机号", domain.ErrInvalidRequest)
	}
	request.Email = strings.ToLower(address.Address)
	switch request.Role {
	case auth.RoleUser, auth.RoleMerchant, auth.RoleRider, auth.RoleAdmin:
	default:
		return nil, fmt.Errorf("%w: unsupported role", domain.ErrInvalidRequest)
	}
	return s.users.CreateUser(ctx, request)
}

func (s *UserService) Register(ctx context.Context, request user.RegisterRequest) (*user.User, error) {
	return s.CreateAccount(ctx, user.CreateRequest{Email: request.Email, Password: request.Password, Name: request.Name, Phone: request.Phone, Role: auth.RoleUser})
}

// PurgePasswordResets removes expired reset tokens so unused requests do not
// accumulate forever. The caller supplies a grace period for incident review.
func (s *UserService) PurgePasswordResets(ctx context.Context, before time.Time, batch, maxPasses int) (int64, error) {
	if batch <= 0 {
		batch = 1000
	}
	if maxPasses <= 0 {
		maxPasses = 5
	}
	var total int64
	for pass := 0; pass < maxPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := s.users.PurgePasswordResets(ctx, before, batch)
		if err != nil {
			return total, fmt.Errorf("purge password resets: %w", err)
		}
		total += deleted
		if deleted < int64(batch) {
			break
		}
	}
	return total, nil
}

func (s *UserService) RequestPasswordReset(ctx context.Context, email string) (*user.PasswordResetResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil {
		return nil, fmt.Errorf("%w: 请输入有效邮箱", domain.ErrInvalidRequest)
	}
	email = strings.ToLower(address.Address)
	result := &user.PasswordResetResult{Email: email, Message: "如果该邮箱已注册，重置码已生成。请到重置页填写邮箱、重置码和新密码。"}
	account, err := s.users.UserByEmail(ctx, email)
	if err != nil {
		if err == domain.ErrNotFound {
			return result, nil
		}
		return nil, err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := passwordResetHash(token)
	expires := time.Now().UTC().Add(15 * time.Minute)
	if err := s.users.CreatePasswordReset(ctx, user.PasswordReset{TokenHash: hash, UserID: account.ID, Email: account.Email, ExpiresAt: expires, CreatedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	result.ExpiresAt = expires
	if s.exposeResetToken {
		result.DevToken = token
		result.Message = "本地演示环境已生成重置码，请复制到下方重置表单。正式部署应通过邮件发送，不要展示给请求者。"
	}
	return result, nil
}

func (s *UserService) ResetPassword(ctx context.Context, request user.PasswordResetConfirm) error {
	email := strings.ToLower(strings.TrimSpace(request.Email))
	token := strings.TrimSpace(request.Token)
	if email == "" || token == "" || len(request.NewPassword) < 8 {
		return fmt.Errorf("%w: 邮箱、重置码和新密码（至少 8 位）均必填", domain.ErrInvalidRequest)
	}
	reset, err := s.users.PasswordReset(ctx, passwordResetHash(token))
	if err != nil {
		if err == domain.ErrNotFound {
			return fmt.Errorf("%w: 重置码无效或已使用", domain.ErrInvalidRequest)
		}
		return err
	}
	if !strings.EqualFold(reset.Email, email) || time.Now().UTC().After(reset.ExpiresAt) {
		_ = s.users.DeletePasswordReset(ctx, reset.TokenHash)
		return fmt.Errorf("%w: 重置码已过期或邮箱不匹配", domain.ErrInvalidRequest)
	}
	hash, err := security.HashPassword(request.NewPassword)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, reset.UserID, hash); err != nil {
		return err
	}
	if err := s.users.DeletePasswordReset(ctx, reset.TokenHash); err != nil {
		return err
	}
	return nil
}

func passwordResetHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *UserService) ListAccounts(ctx context.Context, status string, page repository.Page) ([]*user.User, error) {
	return s.users.ListUsers(ctx, strings.ToUpper(strings.TrimSpace(status)), page)
}

func (s *UserService) UpdateAccount(ctx context.Context, id string, request user.UpdateRequest) (*user.User, error) {
	return s.updateAccount(ctx, id, request)
}

func (s *UserService) UpdateOwnProfile(ctx context.Context, id string, request user.UpdateRequest) (*user.User, error) {
	return s.updateAccount(ctx, id, request)
}

func (s *UserService) updateAccount(ctx context.Context, id string, request user.UpdateRequest) (*user.User, error) {
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	request.Name = strings.TrimSpace(request.Name)
	request.Phone = strings.TrimSpace(request.Phone)
	request.AvatarURL = strings.TrimSpace(request.AvatarURL)
	address, err := mail.ParseAddress(request.Email)
	if err != nil || request.Name == "" || !accountEmailPattern.MatchString(request.Email) || (request.Phone != "" && !accountPhonePattern.MatchString(request.Phone)) {
		return nil, fmt.Errorf("%w: 请填写有效邮箱和姓名；手机号如填写需为 11 位中国大陆手机号", domain.ErrInvalidRequest)
	}
	request.Email = strings.ToLower(address.Address)
	return s.users.UpdateUser(ctx, id, request)
}

func (s *UserService) DisableAccount(ctx context.Context, id string) (*user.User, error) {
	result, err := s.users.SetUserStatus(ctx, id, "DISABLED")
	if err == nil && s.mapInvalidation != nil {
		s.mapInvalidation()
	}
	return result, err
}
