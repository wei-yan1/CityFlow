package promotionredis_test

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	promotionredis "cityflow/app/promotion/internal/adapter/redis"
	"cityflow/app/promotion/internal/application/promotionapp"
	"cityflow/app/promotion/internal/domain/coupon"
)

func testRedis(t *testing.T) *goredis.Client {
	t.Helper()
	addr := os.Getenv("CITYFLOW_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := goredis.NewClient(&goredis.Options{Addr: addr, DB: 15})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis %s 不可用: %v", addr, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestCacheWarmCheckAndConsume(t *testing.T) {
	ctx := context.Background()
	client := testRedis(t)
	cache := promotionredis.New(client)
	id := "test-coupon-" + time.Now().Format("150405.000000")
	t.Cleanup(func() { _ = client.Del(ctx, cache.Key(id)).Err() })

	now := time.Now().UTC()
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	item := &coupon.Coupon{
		ID: id, Status: coupon.StatusActive, TotalQuantity: 10, ClaimedCount: 3,
		PerUserLimit: 1, StartsAt: &start, EndsAt: &end,
	}
	if err := cache.Warm(ctx, item); err != nil {
		t.Fatalf("warm: %v", err)
	}
	decision, reason, err := cache.Check(ctx, id, now)
	if err != nil || decision != promotionapp.GatePass {
		t.Fatalf("check pass: decision=%v reason=%q err=%v", decision, reason, err)
	}
	if remaining, ok, err := cache.Remaining(ctx, id); err != nil || !ok || remaining != 7 {
		t.Fatalf("remaining after warm = %d ok=%v err=%v, want 7", remaining, ok, err)
	}
	if err := cache.Consume(ctx, id); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if remaining, _, _ := cache.Remaining(ctx, id); remaining != 6 {
		t.Fatalf("remaining after consume = %d, want 6", remaining)
	}

	if err := client.HSet(ctx, cache.Key(id), "remaining", 0).Err(); err != nil {
		t.Fatal(err)
	}
	decision, reason, err = cache.Check(ctx, id, now)
	if err != nil || decision != promotionapp.GateReject || reason != "该券已领完" {
		t.Fatalf("sold out: decision=%v reason=%q err=%v", decision, reason, err)
	}

	ended := now.Add(-time.Minute)
	item.EndsAt = &ended
	if err := cache.Warm(ctx, item); err != nil {
		t.Fatal(err)
	}
	decision, reason, err = cache.Check(ctx, id, now)
	if err != nil || decision != promotionapp.GateReject || reason != "该券已结束" {
		t.Fatalf("ended: decision=%v reason=%q err=%v", decision, reason, err)
	}

	decision, _, err = cache.Check(ctx, "missing-"+id, now)
	if err != nil || decision != promotionapp.GateMiss {
		t.Fatalf("missing key: decision=%v err=%v", decision, err)
	}

	if err := cache.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if exists := client.Exists(ctx, cache.Key(id)).Val(); exists != 0 {
		t.Fatalf("key still exists after delete")
	}
}

func TestCacheConsumeIgnoresMissingKey(t *testing.T) {
	ctx := context.Background()
	cache := promotionredis.New(testRedis(t))
	if err := cache.Consume(ctx, "no-such-coupon"); err != nil {
		t.Fatalf("consume missing key should be a no-op, got %v", err)
	}
}
