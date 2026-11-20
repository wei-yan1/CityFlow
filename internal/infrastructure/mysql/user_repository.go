package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/user"
	"cityflow/internal/security"
)

func (s *Store) Authenticate(ctx context.Context, email, password string) (*user.User, error) {
	var record userRecord
	err := s.orm.WithContext(ctx).Where("email = ?", strings.ToLower(strings.TrimSpace(email))).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("load login account: %w", err)
	}
	if record.Status != "ACTIVE" || !security.VerifyPassword(record.PasswordHash, password) {
		return nil, domain.ErrUnauthorized
	}
	now := time.Now().UTC()
	if err := s.orm.WithContext(ctx).Model(&userRecord{}).Where("id = ?", record.ID).Update("last_login_at", now).Error; err != nil {
		return nil, fmt.Errorf("update last login: %w", err)
	}
	return toUser(record), nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*user.User, error) {
	var record userRecord
	err := s.orm.WithContext(ctx).Select("id", "email", "display_name", "phone", "avatar_url", "role", "status", "created_at", "updated_at").Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	return toUser(record), nil
}

func (s *Store) IsActive(ctx context.Context, id string) (bool, error) {
	var record userRecord
	err := s.orm.WithContext(ctx).Select("id").Where("id = ? AND status = ?", id, "ACTIVE").Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) CreateUser(ctx context.Context, request user.CreateRequest) (*user.User, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	hash, err := security.HashPassword(request.Password)
	if err != nil {
		return nil, err
	}
	record := userRecord{ID: id, Email: request.Email, Phone: optionalString(request.Phone), PasswordHash: hash,
		DisplayName: request.Name, Role: request.Role, Status: "ACTIVE"}
	if err := s.orm.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return s.GetUser(ctx, id)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*user.User, error) {
	var record userRecord
	err := s.orm.WithContext(ctx).Where("email = ?", strings.ToLower(strings.TrimSpace(email))).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load user by email: %w", err)
	}
	return toUser(record), nil
}

func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	result := s.orm.WithContext(ctx).Model(&userRecord{}).Where("id = ?", userID).Update("password_hash", passwordHash)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CreatePasswordReset(ctx context.Context, reset user.PasswordReset) error {
	record := passwordResetRecord{TokenHash: reset.TokenHash, UserID: reset.UserID, Email: reset.Email, ExpiresAt: reset.ExpiresAt, CreatedAt: reset.CreatedAt}
	return s.orm.WithContext(ctx).Create(&record).Error
}

func (s *Store) PasswordReset(ctx context.Context, tokenHash string) (*user.PasswordReset, error) {
	var record passwordResetRecord
	err := s.orm.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load password reset: %w", err)
	}
	return &user.PasswordReset{TokenHash: record.TokenHash, UserID: record.UserID, Email: record.Email, ExpiresAt: record.ExpiresAt, CreatedAt: record.CreatedAt}, nil
}

func (s *Store) DeletePasswordReset(ctx context.Context, tokenHash string) error {
	return s.orm.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&passwordResetRecord{}).Error
}

func (s *Store) ListUsers(ctx context.Context, status string, page repository.Page) ([]*user.User, error) {
	query := s.orm.WithContext(ctx).Model(&userRecord{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var records []userRecord
	err := query.Select("id", "email", "display_name", "phone", "avatar_url", "role", "status", "created_at", "updated_at").
		Order("created_at DESC").Order("id DESC").Scopes(paginate(page)).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	result := make([]*user.User, 0)
	for _, record := range records {
		result = append(result, toUser(record))
	}
	return result, nil
}

func (s *Store) UpdateUser(ctx context.Context, id string, request user.UpdateRequest) (*user.User, error) {
	result := s.orm.WithContext(ctx).Model(&userRecord{}).Where("id = ?", id).Updates(map[string]any{
		"email": request.Email, "display_name": request.Name,
		"phone": optionalString(request.Phone), "avatar_url": optionalString(request.AvatarURL),
	})
	if result.Error != nil {
		return nil, writeError(result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := s.GetUser(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.GetUser(ctx, id)
}

func (s *Store) SetUserStatus(ctx context.Context, id, status string) (*user.User, error) {
	result := s.orm.WithContext(ctx).Model(&userRecord{}).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := s.GetUser(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.GetUser(ctx, id)
}
