package authn

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestStatusCacheReusesAnswerWithinTTL(t *testing.T) {
	var calls atomic.Int32
	cache := NewStatusCache(time.Minute, func(context.Context, string, string) (bool, error) {
		calls.Add(1)
		return true, nil
	})

	active, err := cache.IsActive(context.Background(), "user-1", "token-1")
	if err != nil || !active {
		t.Fatalf("first check = %v, %v", active, err)
	}
	active, err = cache.IsActive(context.Background(), "user-1", "token-2")
	if err != nil || !active {
		t.Fatalf("second check = %v, %v", active, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("authority calls = %d, want 1", got)
	}
}

func TestStatusCacheExpiresAndCachesInactive(t *testing.T) {
	var calls atomic.Int32
	activeValue := true
	cache := NewStatusCache(time.Second, func(context.Context, string, string) (bool, error) {
		calls.Add(1)
		return activeValue, nil
	})
	now := time.Now().UTC()
	cache.now = func() time.Time { return now }

	if active, err := cache.IsActive(context.Background(), "user-2", "token"); err != nil || !active {
		t.Fatalf("initial check = %v, %v", active, err)
	}
	activeValue = false
	now = now.Add(2 * time.Second)
	if active, err := cache.IsActive(context.Background(), "user-2", "token"); err != nil || active {
		t.Fatalf("expired check = %v, %v", active, err)
	}
	if active, err := cache.IsActive(context.Background(), "user-2", "token"); err != nil || active {
		t.Fatalf("cached inactive check = %v, %v", active, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("authority calls = %d, want 2", got)
	}
}

func TestStatusCacheDoesNotCacheAuthorityFailure(t *testing.T) {
	var calls atomic.Int32
	cache := NewStatusCache(time.Minute, func(context.Context, string, string) (bool, error) {
		calls.Add(1)
		return false, errors.New("identity unavailable")
	})
	if _, err := cache.IsActive(context.Background(), "user-3", "token"); err == nil {
		t.Fatal("expected authority error")
	}
	if _, err := cache.IsActive(context.Background(), "user-3", "token"); err == nil {
		t.Fatal("expected authority error on retry")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("authority calls = %d, want 2", got)
	}
}

func TestStatusCacheRememberSeedsActive(t *testing.T) {
	var calls atomic.Int32
	cache := NewStatusCache(time.Minute, func(context.Context, string, string) (bool, error) {
		calls.Add(1)
		return false, nil
	})
	cache.Remember("user-4", true)
	if active, err := cache.IsActive(context.Background(), "user-4", ""); err != nil || !active {
		t.Fatalf("remembered status = %v, %v", active, err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("authority calls = %d, want 0", got)
	}
}
