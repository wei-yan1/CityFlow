// Package identitymysql is the Identity service's MySQL adapter. It owns the
// users, password-reset and identity-outbox tables and never queries data
// belonging to another service.
package identitymysql

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"cityflow/app/identity/domain/user"
	"cityflow/app/identity/repository"
	"cityflow/internal/common/domain"
	platformmysql "cityflow/internal/platform/mysql"
	"cityflow/internal/security"
)

type Store struct {
	db *platformmysql.DB
}

func New(db *platformmysql.DB) *Store { return &Store{db: db} }

var _ repository.AuthRepository = (*Store)(nil)
var _ repository.UserRepository = (*Store)(nil)

type userRecord struct {
	ID           string     `gorm:"column:id;type:char(36);primaryKey"`
	Email        string     `gorm:"column:email;size:254;not null;uniqueIndex:users_email_unique"`
	Phone        *string    `gorm:"column:phone;size:32;uniqueIndex:users_phone_unique"`
	PasswordHash string     `gorm:"column:password_hash;size:255;not null"`
	DisplayName  string     `gorm:"column:display_name;size:80;not null"`
	AvatarURL    *string    `gorm:"column:avatar_url;type:text"`
	Role         string     `gorm:"column:role;size:20;not null"`
	Status       string     `gorm:"column:status;size:20;not null"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

func (userRecord) TableName() string { return "users" }

type passwordResetRecord struct {
	TokenHash string    `gorm:"column:token_hash;type:char(64);primaryKey"`
	UserID    string    `gorm:"column:user_id;type:char(36);not null"`
	Email     string    `gorm:"column:email;size:254;not null"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (passwordResetRecord) TableName() string { return "password_reset_tokens" }

func (s *Store) Authenticate(ctx context.Context, email, password string) (*user.User, error) {
	var record userRecord
	err := s.db.ORM.WithContext(ctx).Where("email = ?", strings.ToLower(strings.TrimSpace(email))).Take(&record).Error
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
	if err := s.db.ORM.WithContext(ctx).Model(&userRecord{}).Where("id = ?", record.ID).Update("last_login_at", now).Error; err != nil {
		return nil, fmt.Errorf("update last login: %w", err)
	}
	return toUser(record), nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*user.User, error) {
	var record userRecord
	err := s.db.ORM.WithContext(ctx).Select("id", "email", "display_name", "phone", "avatar_url", "role", "status", "created_at", "updated_at").Where("id = ?", id).Take(&record).Error
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
	err := s.db.ORM.WithContext(ctx).Select("id").Where("id = ? AND status = ?", id, "ACTIVE").Take(&record).Error
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
	if err := s.db.ORM.WithContext(ctx).Create(&record).Error; err != nil {
		return nil, writeError(err)
	}
	return s.GetUser(ctx, id)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*user.User, error) {
	var record userRecord
	err := s.db.ORM.WithContext(ctx).Where("email = ?", strings.ToLower(strings.TrimSpace(email))).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load user by email: %w", err)
	}
	return toUser(record), nil
}

func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	result := s.db.ORM.WithContext(ctx).Model(&userRecord{}).Where("id = ?", userID).Update("password_hash", passwordHash)
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
	return s.db.ORM.WithContext(ctx).Create(&record).Error
}

func (s *Store) PasswordReset(ctx context.Context, tokenHash string) (*user.PasswordReset, error) {
	var record passwordResetRecord
	err := s.db.ORM.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load password reset: %w", err)
	}
	return &user.PasswordReset{TokenHash: record.TokenHash, UserID: record.UserID, Email: record.Email, ExpiresAt: record.ExpiresAt, CreatedAt: record.CreatedAt}, nil
}

func (s *Store) DeletePasswordReset(ctx context.Context, tokenHash string) error {
	return s.db.ORM.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&passwordResetRecord{}).Error
}

// PurgePasswordResets removes expired reset tokens in bounded batches. Use
// stays a single-row delete; this is the background retention path.
func (s *Store) PurgePasswordResets(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := s.db.ORM.WithContext(ctx).Exec(
		"DELETE FROM password_reset_tokens WHERE expires_at < ? LIMIT ?",
		before, limit,
	)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (s *Store) ListUsers(ctx context.Context, status string, page repository.Page) ([]*user.User, error) {
	query := s.db.ORM.WithContext(ctx).Model(&userRecord{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var records []userRecord
	err := query.Select("id", "email", "display_name", "phone", "avatar_url", "role", "status", "created_at", "updated_at").
		Order("created_at DESC").Order("id DESC").Scopes(paginate(page)).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	result := make([]*user.User, 0, len(records))
	for _, record := range records {
		result = append(result, toUser(record))
	}
	return result, nil
}

func (s *Store) UpdateUser(ctx context.Context, id string, request user.UpdateRequest) (*user.User, error) {
	result := s.db.ORM.WithContext(ctx).Model(&userRecord{}).Where("id = ?", id).Updates(map[string]any{
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
	result := s.db.ORM.WithContext(ctx).Model(&userRecord{}).Where("id = ?", id).Update("status", status)
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

func toUser(record userRecord) *user.User {
	return &user.User{
		ID: record.ID, Email: record.Email, Name: record.DisplayName,
		Phone: stringValue(record.Phone), AvatarURL: stringValue(record.AvatarURL),
		Role: record.Role, Status: record.Status, CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt, PasswordHash: record.PasswordHash,
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func serviceTypesJSON(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := fmt.Sprintf("%x", raw)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func paginate(page repository.Page) func(*gorm.DB) *gorm.DB {
	if page.Limit <= 0 || page.Limit > 200 {
		page.Limit = 50
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return func(db *gorm.DB) *gorm.DB { return db.Limit(page.Limit).Offset(page.Offset) }
}

func writeError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("%w: duplicate unique value", domain.ErrConflict)
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("%w: duplicate unique value", domain.ErrConflict)
	}
	return err
}
