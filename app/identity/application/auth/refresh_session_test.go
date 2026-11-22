package authservice

import (
	"context"
	"errors"
	"os"
	"testing"

	identityredis "cityflow/app/identity/internal/adapter/redis"
	"cityflow/internal/common/domain"
	"cityflow/internal/platform/cache"
	"cityflow/internal/testfixture/memory"
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
	refreshStore := identityredis.NewRefreshStore(redis)
	ctx := context.Background()
	svc := NewAuthService(store, []byte("test-secret"))
	id := "00000000-0000-4000-8000-000000000001"
	first, err := svc.StartRefreshSession(ctx, refreshStore, id)
	if err != nil {
		t.Fatal(err)
	}
	result, next, err := svc.RefreshSession(ctx, refreshStore, first)
	if err != nil || result.User.ID != id || next == first {
		t.Fatalf("refresh: %v", err)
	}
	defer refreshStore.RevokeRefreshSession(ctx, next)
	if _, _, err = svc.RefreshSession(ctx, refreshStore, first); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("replay: %v", err)
	}
	if _, err = store.SetUserStatus(ctx, id, "DISABLED"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.RefreshSession(ctx, refreshStore, next); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled: %v", err)
	}
	if _, _, err = svc.RefreshSession(ctx, refreshStore, next); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled token reuse: %v", err)
	}
}
