package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshRotationIsAtomicAndRevocable(t *testing.T) {
	address := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR")
	}
	r := NewRedis(address, "", 0)
	defer r.Close()
	ctx := context.Background()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	prefix := hex.EncodeToString(random)
	old := prefix + "-old"
	if err := r.CreateRefreshSession(ctx, old, "user-1", time.Minute); err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			next := prefix + string(rune('a'+i))
			defer r.RevokeRefreshSession(ctx, next)
			user, err := r.RotateRefreshSession(ctx, old, next, time.Minute)
			if err == nil {
				if user != "user-1" {
					t.Errorf("wrong user %q", user)
				}
				count.Add(1)
				if err = r.RevokeRefreshSession(ctx, next); err != nil {
					t.Error(err)
				}
				if _, err = r.RotateRefreshSession(ctx, next, next+"x", time.Minute); !errors.Is(err, ErrRefreshInvalid) {
					t.Errorf("revoked token accepted: %v", err)
				}
			} else if !errors.Is(err, ErrRefreshInvalid) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("successful rotations=%d", count.Load())
	}
}
