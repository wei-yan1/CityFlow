package identitymysql

import (
	"context"
	"fmt"

	"cityflow/internal/common/auth"
	"cityflow/internal/security"
)

// SeedDemoIdentity inserts the four demo accounts only when the users table is
// empty. The caller must pass the password explicitly (CITYFLOW_DEMO_PASSWORD);
// no password is compiled into the binary. The unique indexes make a retry safe
// even if two instances race.
func (s *Store) SeedDemoIdentity(ctx context.Context, password string) error {
	var count int64
	if err := s.db.ORM.WithContext(ctx).Model(&userRecord{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	seeds := []struct{ id, email, name, phone, role string }{
		{"00000000-0000-4000-8000-000000000001", "user@cityflow.local", "林知夏", "13800000001", auth.RoleUser},
		{"00000000-0000-4000-8000-000000000002", "merchant@cityflow.local", "校园便利店", "13800000002", auth.RoleMerchant},
		{"00000000-0000-4000-8000-000000000003", "rider@cityflow.local", "周远", "13800000003", auth.RoleRider},
		{"00000000-0000-4000-8000-000000000004", "admin@cityflow.local", "平台管理员", "13800000004", auth.RoleAdmin},
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	for _, item := range seeds {
		record := userRecord{ID: item.id, Email: item.email, Phone: &item.phone, PasswordHash: hash,
			DisplayName: item.name, Role: item.role, Status: "ACTIVE"}
		if err := s.db.ORM.WithContext(ctx).Create(&record).Error; err != nil {
			return fmt.Errorf("seed identity user %s: %w", item.email, err)
		}
	}
	return nil
}
