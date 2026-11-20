package mysqlstore

import (
	"context"
	"fmt"

	"cityflow/internal/domain/auth"
	"cityflow/internal/security"
)

// SeedDemoIdentity seeds only accounts for the Identity service database.
func (s *Store) SeedDemoIdentity(ctx context.Context) error {
	conn, release, err := s.withLock(ctx, "cityflow_seed_demo_identity")
	if err != nil {
		return err
	}
	defer release()
	var count int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	seeds := []struct{ id, email, name, phone, role string }{
		{"00000000-0000-4000-8000-000000000001", "user@cityflow.local", "示例用户", "13800000001", auth.RoleUser},
		{"00000000-0000-4000-8000-000000000002", "merchant@cityflow.local", "示例商家", "13800000002", auth.RoleMerchant},
		{"00000000-0000-4000-8000-000000000003", "rider@cityflow.local", "示例骑手", "13800000003", auth.RoleRider},
		{"00000000-0000-4000-8000-000000000004", "admin@cityflow.local", "平台管理员", "13800000004", auth.RoleAdmin},
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, item := range seeds {
		hash, err := security.HashPassword("cityflow123")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO users(id,email,phone,password_hash,display_name,role,status)
			VALUES(?,?,?,?,?,?,'ACTIVE')`, item.id, item.email, item.phone, hash, item.name, item.role); err != nil {
			return fmt.Errorf("seed identity user %s: %w", item.email, err)
		}
	}
	return tx.Commit()
}
