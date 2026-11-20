package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"cityflow/internal/domain"
	"cityflow/internal/infrastructure/cache"
	"cityflow/internal/infrastructure/memory"
)

func TestRefreshSessionRotationAndDisabledAccount(t *testing.T) {
	address := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR")
	}
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	redis := cache.NewRedis(address, "", 0)
	defer redis.Close()
	ctx := context.Background()
	svc := NewAuthService(store, store, store, []byte("test-secret"))
	id := "00000000-0000-4000-8000-000000000001"
	first, err := svc.StartRefreshSession(ctx, redis, id)
	if err != nil {
		t.Fatal(err)
	}
	result, next, err := svc.RefreshSession(ctx, redis, first)
	if err != nil || result.User.ID != id || next == first {
		t.Fatalf("refresh: %v", err)
	}
	defer redis.RevokeRefreshSession(ctx, next)
	if _, _, err = svc.RefreshSession(ctx, redis, first); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("replay: %v", err)
	}
	if _, err = store.SetUserStatus(ctx, id, "DISABLED"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.RefreshSession(ctx, redis, next); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled: %v", err)
	}
	if _, _, err = svc.RefreshSession(ctx, redis, next); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled token reuse: %v", err)
	}
}
