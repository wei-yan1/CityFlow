package mysqlstore

import (
	"cityflow/internal/domain"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/user"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestMerchantArchiveAndRecreateMySQL(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("dedicated local MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.MigrateAllServices(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	account, err := s.CreateUser(ctx, user.CreateRequest{Email: id + "@example.test", Password: "test-password", Name: "merchant self", Role: "MERCHANT"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM users WHERE id=?", account.ID)
	defer s.orm.Exec("DELETE FROM merchant_profiles WHERE user_id=?", account.ID)
	body := merchant.UpdateRequest{Name: "initial", Address: "A", Latitude: 31.23, Longitude: 121.47, BusinessStatus: "OPEN", ServiceTypes: []string{"DELIVERY"}, CapacityPerHour: 60}
	original, err := s.CreateMerchant(ctx, account.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateMerchant(ctx, account.ID, body); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate=%v", err)
	}
	if err = s.ArchiveMerchant(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MerchantForUser(ctx, account.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("archived lookup=%v", err)
	}
	body.Name = "restored"
	body.Latitude = 31.24
	body.CapacityPerHour = 0
	restored, err := s.CreateMerchant(ctx, account.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != original.ID || restored.Name != "restored" || restored.Latitude != 31.24 || restored.CapacityPerHour != 0 {
		t.Fatalf("restore=%+v", restored)
	}
	reopened, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err = reopened.MerchantForUser(ctx, account.ID)
	if err != nil || restored.ID != original.ID {
		t.Fatalf("durability=%+v %v", restored, err)
	}
}
