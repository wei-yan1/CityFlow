package promotionapp_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	promotionredis "cityflow/app/promotion/internal/adapter/redis"
	"cityflow/app/promotion/internal/application/promotionapp"
	"cityflow/app/promotion/internal/domain/coupon"
)

type fakeClaimStore struct {
	mu         sync.Mutex
	err        error
	batchErr   error
	items      map[string]*coupon.UserCoupon
	calls      int
	batchCalls int
}

func newFakeClaimStore() *fakeClaimStore {
	return &fakeClaimStore{items: map[string]*coupon.UserCoupon{}}
}

func (f *fakeClaimStore) ClaimCoupon(_ context.Context, userID, couponID, idempotencyKey string, _ time.Time) (*coupon.UserCoupon, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	id := "claim-" + idempotencyKey
	item := &coupon.UserCoupon{ID: id, CouponID: couponID, UserID: userID, Status: coupon.ClaimUnused}
	f.items[id] = item
	return item, nil
}

func (f *fakeClaimStore) UserCoupon(_ context.Context, _ string, id string) (*coupon.UserCoupon, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if item, ok := f.items[id]; ok {
		return item, nil
	}
	return nil, coupon.ErrNotFound
}

func (f *fakeClaimStore) ClaimCouponBatch(ctx context.Context, requests []promotionapp.ClaimRequest, now time.Time) ([]promotionapp.ClaimResult, error) {
	f.mu.Lock()
	f.batchCalls++
	batchErr := f.batchErr
	f.mu.Unlock()
	if batchErr != nil {
		return nil, batchErr
	}
	results := make([]promotionapp.ClaimResult, len(requests))
	for index, request := range requests {
		item, err := f.ClaimCoupon(ctx, request.UserID, request.CouponID, request.IdempotencyKey, now)
		results[index] = promotionapp.ClaimResult{Item: item, Err: err}
	}
	return results, nil
}

func redisClient(t *testing.T) *goredis.Client {
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

type asyncFixture struct {
	client   *goredis.Client
	cache    *promotionredis.Cache
	stream   *promotionredis.ClaimStream
	couponID string
	coupon   *coupon.Coupon
}

func newAsyncFixture(t *testing.T) *asyncFixture {
	t.Helper()
	client := redisClient(t)
	id := fmt.Sprintf("test-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	item := &coupon.Coupon{
		ID: id, Status: coupon.StatusActive, TotalQuantity: 10, PerUserLimit: 1,
		StartsAt: &start, EndsAt: &end,
	}
	cache := promotionredis.New(client)
	if err := cache.Warm(context.Background(), item); err != nil {
		t.Fatalf("warm: %v", err)
	}
	stream := promotionredis.NewClaimStream(client, "prs:claim:test:"+id, "promotion-test", time.Hour)
	f := &asyncFixture{client: client, cache: cache, stream: stream, couponID: id, coupon: item}
	t.Cleanup(func() {
		ctx := context.Background()
		_ = client.Del(ctx, cache.Key(id)).Err()
		_ = client.Del(ctx, "prs:user:coupon:"+id).Err()
		_ = client.Del(ctx, "prs:claim:test:"+id).Err()
		_ = client.Del(ctx,
			stream.IdemKey("user-a", id, "idem-a"),
			stream.IdemKey("user-a", id, "idem-b"),
			stream.IdemKey("user-b", id, "idem-b"),
			stream.IdemKey("user-b", id, "idem-c"),
			stream.IdemKey("user-c", id, "idem-c"),
			stream.IdemKey("user-a", id, "idem-0"),
			stream.IdemKey("user-b", id, "idem-1"),
			stream.IdemKey("user-c", id, "idem-2"),
			stream.IdemKey("user-a", id, "trim-a"),
			stream.IdemKey("user-b", id, "trim-b"),
			stream.IdemKey("user-a", id, "batch-a"),
			stream.IdemKey("user-b", id, "batch-b"),
			stream.IdemKey("user-c", id, "batch-c"),
			stream.IdemKey("user-a", id, "same"),
			stream.IdemKey("user-b", id, "same"),
			stream.IdemKey("user-a", "not-cached-coupon", "idem-x"),
		).Err()
	})
	return f
}

func TestReservationIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}

	first, err := f.stream.Reserve(ctx, "user-a", f.couponID, "idem-a", "claim-a", now)
	if err != nil {
		t.Fatalf("reserve first: %v", err)
	}
	if first.Status != "ok" || first.EventID == "" {
		t.Fatalf("first reservation = %+v", first)
	}
	if remaining := f.remaining(t); remaining != 9 {
		t.Fatalf("remaining = %d, want 9", remaining)
	}
	if used := f.client.HGet(ctx, "prs:user:coupon:"+f.couponID, "user-a").Val(); used != "1" {
		t.Fatalf("user count = %q, want 1", used)
	}
	if length := f.client.XLen(ctx, f.stream.Stream()).Val(); length != 1 {
		t.Fatalf("stream length = %d, want 1", length)
	}

	replay, err := f.stream.Reserve(ctx, "user-a", f.couponID, "idem-a", "claim-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Status != "pending" {
		t.Fatalf("replay status = %q, want pending", replay.Status)
	}
	if remaining := f.remaining(t); remaining != 9 {
		t.Fatalf("replay changed remaining = %d", remaining)
	}
	if length := f.client.XLen(ctx, f.stream.Stream()).Val(); length != 1 {
		t.Fatalf("replay enqueued another event, length=%d", length)
	}

	limit, err := f.stream.Reserve(ctx, "user-a", f.couponID, "idem-b", "claim-b", now)
	if err != nil {
		t.Fatal(err)
	}
	if limit.Status != "reject" || limit.Reason != "limit" {
		t.Fatalf("limit reservation = %+v", limit)
	}
	if remaining := f.remaining(t); remaining != 9 {
		t.Fatalf("limit rejection changed remaining = %d", remaining)
	}

	secondUser, err := f.stream.Reserve(ctx, "user-b", f.couponID, "idem-c", "claim-c", now)
	if err != nil {
		t.Fatal(err)
	}
	if secondUser.Status != "ok" {
		t.Fatalf("second user reservation = %+v", secondUser)
	}
	if remaining := f.remaining(t); remaining != 8 {
		t.Fatalf("remaining = %d, want 8", remaining)
	}

	if err := f.stream.Release(ctx, first, "soldout"); err != nil {
		t.Fatal(err)
	}
	if remaining := f.remaining(t); remaining != 9 {
		t.Fatalf("release did not restore stock, remaining=%d", remaining)
	}
	if used := f.client.HGet(ctx, "prs:user:coupon:"+f.couponID, "user-a").Val(); used != "0" {
		t.Fatalf("user count after release = %q, want 0", used)
	}
	result, found, err := f.stream.Result(ctx, "user-a", f.couponID, "idem-a")
	if err != nil || !found || result.Status != "failed" || result.Reason != "soldout" {
		t.Fatalf("result after release = %+v found=%v err=%v", result, found, err)
	}
}

func TestReservationIdempotencyScopedByUserAndCoupon(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()

	first, err := f.stream.Reserve(ctx, "user-a", f.couponID, "same", "claim-a", now)
	if err != nil || first.Status != "ok" {
		t.Fatalf("user-a reserve: %+v err=%v", first, err)
	}
	// 不同用户碰巧使用同一个客户端键，必须各自成立，而不是被判成 duplicate。
	second, err := f.stream.Reserve(ctx, "user-b", f.couponID, "same", "claim-b", now)
	if err != nil || second.Status != "ok" {
		t.Fatalf("user-b reserve: %+v err=%v", second, err)
	}
	if remaining := f.remaining(t); remaining != 8 {
		t.Fatalf("remaining=%d, want 8", remaining)
	}
	for _, user := range []string{"user-a", "user-b"} {
		result, found, err := f.stream.Result(ctx, user, f.couponID, "same")
		if err != nil || !found || result.Status != "pending" || result.UserID != user {
			t.Fatalf("result for %s: %+v found=%v err=%v", user, result, found, err)
		}
	}
}

func TestReservationRejectsWhenStreamFull(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	f.stream.WithMaxLen(2)
	now := time.Now().UTC()
	for index, user := range []string{"user-a", "user-b"} {
		reservation, err := f.stream.Reserve(ctx, user, f.couponID, fmt.Sprintf("idem-%d", index), "claim-"+user, now)
		if err != nil || reservation.Status != "ok" {
			t.Fatalf("reserve %s: %+v err=%v", user, reservation, err)
		}
	}
	// 前两条只扣库存、未消费；队列达到上限后第三条必须被拒绝，不能再扣库存。
	overflow, err := f.stream.Reserve(ctx, "user-c", f.couponID, "idem-2", "claim-user-c", now)
	if err != nil || overflow.Status != "reject" || overflow.Reason != "busy" {
		t.Fatalf("overflow reservation: %+v err=%v", overflow, err)
	}
	if remaining := f.remaining(t); remaining != 8 {
		t.Fatalf("remaining=%d, want 8 (overflow must not consume)", remaining)
	}
}

func TestStreamTrimKeepsLengthBoundedAfterAck(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	for index, user := range []string{"user-a", "user-b"} {
		if _, err := f.stream.Reserve(ctx, user, f.couponID, fmt.Sprintf("trim-%c", 'a'+index), "claim-"+user, now); err != nil {
			t.Fatal(err)
		}
	}
	items, err := f.stream.Read(ctx, "trim-consumer", 10, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("read len=%d err=%v", len(items), err)
	}
	for _, item := range items {
		if err := f.stream.Ack(ctx, item.EventID); err != nil {
			t.Fatal(err)
		}
	}
	f.stream.WithMaxLen(1)
	if err := f.stream.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	if length := f.client.XLen(ctx, f.stream.Stream()).Val(); length != 1 {
		t.Fatalf("stream length=%d after trim, want 1", length)
	}
}

func TestReservationBeforeGroupCreationIsNotLost(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	// 模拟服务启动：先有请求预扣成功，消费者组还没建。
	reservation, err := f.stream.Reserve(ctx, "user-a", f.couponID, "idem-a", "claim-a", time.Now().UTC())
	if err != nil || reservation.Status != "ok" {
		t.Fatalf("reserve before group: %+v err=%v", reservation, err)
	}
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := f.stream.Read(ctx, "test-consumer", 10, 0)
	if err != nil || len(items) != 1 || items[0].EventID != reservation.EventID {
		t.Fatalf("read after group creation: len=%d items=%+v err=%v", len(items), items, err)
	}
}

func TestWorkerCommitAndAsyncClaimerReadsResult(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	store := newFakeClaimStore()
	worker := promotionapp.NewClaimWorker(store, f.stream, f.stream, "test-consumer", 10, time.Second, time.Minute)

	reservation, err := f.stream.Reserve(ctx, "user-a", f.couponID, "idem-a", "claim-a", now)
	if err != nil || reservation.Status != "ok" {
		t.Fatalf("reserve: %+v err=%v", reservation, err)
	}
	items, err := f.stream.Read(ctx, "test-consumer", 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("read: len=%d err=%v", len(items), err)
	}
	worker.Process(ctx, items[0])

	result, found, err := f.stream.Result(ctx, "user-a", f.couponID, "idem-a")
	if err != nil || !found || result.Status != "ok" || result.ClaimID == "" {
		t.Fatalf("result after commit = %+v found=%v err=%v", result, found, err)
	}
	if pending := f.client.XPending(ctx, f.stream.Stream(), f.stream.Group()).Val(); pending == nil || pending.Count != 0 {
		t.Fatalf("pending after ack = %+v", pending)
	}

	item, handled, err := promotionapp.NewAsyncClaimer(store, f.stream, 100*time.Millisecond).
		Claim(ctx, "user-a", f.couponID, "idem-a")
	if err != nil || !handled || item == nil || item.ID != result.ClaimID {
		t.Fatalf("async claim replay: item=%+v handled=%v err=%v", item, handled, err)
	}
}

func TestWorkerCompensatesPermanentFailure(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	store := newFakeClaimStore()
	store.err = fmt.Errorf("%w: 该券已领完", coupon.ErrConflict)
	worker := promotionapp.NewClaimWorker(store, f.stream, f.stream, "test-consumer", 10, time.Second, time.Minute)

	reservation, err := f.stream.Reserve(ctx, "user-a", f.couponID, "idem-a", "claim-a", now)
	if err != nil || reservation.Status != "ok" {
		t.Fatalf("reserve: %+v err=%v", reservation, err)
	}
	if remaining := f.remaining(t); remaining != 9 {
		t.Fatalf("remaining before failure = %d", remaining)
	}
	items, err := f.stream.Read(ctx, "test-consumer", 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("read: len=%d err=%v", len(items), err)
	}
	worker.Process(ctx, items[0])

	if remaining := f.remaining(t); remaining != 10 {
		t.Fatalf("compensation did not restore stock, remaining=%d", remaining)
	}
	if used := f.client.HGet(ctx, "prs:user:coupon:"+f.couponID, "user-a").Val(); used != "0" {
		t.Fatalf("user count after compensation = %q, want 0", used)
	}
	result, found, err := f.stream.Result(ctx, "user-a", f.couponID, "idem-a")
	if err != nil || !found || result.Status != "failed" || result.Reason != "soldout" {
		t.Fatalf("result after failure = %+v found=%v err=%v", result, found, err)
	}
	if pending := f.client.XPending(ctx, f.stream.Stream(), f.stream.Group()).Val(); pending == nil || pending.Count != 0 {
		t.Fatalf("pending after failure ack = %+v", pending)
	}
	// ACK 丢失后同一条消息被重投：已是 failed 终态，不能再跑一次数据库。
	worker.Process(ctx, items[0])
	if store.calls != 1 {
		t.Fatalf("replayed failed reservation called store %d times, want 1", store.calls)
	}
}

func TestWorkerBatchCommitsReservationsInOneStoreCall(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	store := newFakeClaimStore()
	worker := promotionapp.NewClaimWorker(store, f.stream, f.stream, "test-consumer", 10, time.Second, time.Minute)

	for _, user := range []string{"user-a", "user-b", "user-c"} {
		reservation, err := f.stream.Reserve(ctx, user, f.couponID, "batch-"+user, "claim-"+user, now)
		if err != nil || reservation.Status != "ok" {
			t.Fatalf("reserve %s: %+v err=%v", user, reservation, err)
		}
	}
	items, err := f.stream.Read(ctx, "test-consumer", 10, 0)
	if err != nil || len(items) != 3 {
		t.Fatalf("read: len=%d err=%v", len(items), err)
	}
	worker.ProcessBatch(ctx, items)
	if store.batchCalls != 1 {
		t.Fatalf("batch store calls=%d, want 1", store.batchCalls)
	}
	if remaining := f.remaining(t); remaining != 7 {
		t.Fatalf("remaining after batch = %d, want 7", remaining)
	}
	if pending := f.client.XPending(ctx, f.stream.Stream(), f.stream.Group()).Val(); pending == nil || pending.Count != 0 {
		t.Fatalf("pending after batch ack = %+v", pending)
	}
	for _, item := range items {
		result, found, err := f.stream.Result(ctx, item.UserID, f.couponID, item.IdempotencyKey)
		if err != nil || !found || result.Status != "ok" || result.ClaimID == "" {
			t.Fatalf("result for %s = %+v found=%v err=%v", item.UserID, result, found, err)
		}
	}
}

func TestWorkerBatchCompensatesPermanentFailure(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	store := newFakeClaimStore()
	store.err = fmt.Errorf("%w: 该券已领完", coupon.ErrConflict)
	worker := promotionapp.NewClaimWorker(store, f.stream, f.stream, "test-consumer", 10, time.Second, time.Minute)

	for _, user := range []string{"user-a", "user-b", "user-c"} {
		reservation, err := f.stream.Reserve(ctx, user, f.couponID, "batch-"+user, "claim-"+user, now)
		if err != nil || reservation.Status != "ok" {
			t.Fatalf("reserve %s: %+v err=%v", user, reservation, err)
		}
	}
	items, err := f.stream.Read(ctx, "test-consumer", 10, 0)
	if err != nil || len(items) != 3 {
		t.Fatalf("read: len=%d err=%v", len(items), err)
	}
	worker.ProcessBatch(ctx, items)

	if remaining := f.remaining(t); remaining != 10 {
		t.Fatalf("batch compensation did not restore stock, remaining=%d", remaining)
	}
	if used := f.client.HGet(ctx, "prs:user:coupon:"+f.couponID, "user-a").Val(); used != "0" {
		t.Fatalf("user count after batch compensation = %q, want 0", used)
	}
	for _, item := range items {
		result, found, err := f.stream.Result(ctx, item.UserID, f.couponID, item.IdempotencyKey)
		if err != nil || !found || result.Status != "failed" || result.Reason != "soldout" {
			t.Fatalf("result for %s = %+v found=%v err=%v", item.UserID, result, found, err)
		}
	}
	if pending := f.client.XPending(ctx, f.stream.Stream(), f.stream.Group()).Val(); pending == nil || pending.Count != 0 {
		t.Fatalf("pending after batch compensation = %+v", pending)
	}
}

func TestWorkerBatchFallsBackToSingleOnTopLevelError(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	now := time.Now().UTC()
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	store := newFakeClaimStore()
	store.batchErr = fmt.Errorf("deadlock")
	worker := promotionapp.NewClaimWorker(store, f.stream, f.stream, "test-consumer", 10, time.Second, time.Minute)

	for _, user := range []string{"user-a", "user-b", "user-c"} {
		reservation, err := f.stream.Reserve(ctx, user, f.couponID, "batch-"+user, "claim-"+user, now)
		if err != nil || reservation.Status != "ok" {
			t.Fatalf("reserve %s: %+v err=%v", user, reservation, err)
		}
	}
	items, err := f.stream.Read(ctx, "test-consumer", 10, 0)
	if err != nil || len(items) != 3 {
		t.Fatalf("read: len=%d err=%v", len(items), err)
	}
	worker.ProcessBatch(ctx, items)
	if store.batchCalls != 1 || store.calls != 3 {
		t.Fatalf("batch calls=%d single calls=%d, want 1 and 3", store.batchCalls, store.calls)
	}
	if remaining := f.remaining(t); remaining != 7 {
		t.Fatalf("remaining after fallback = %d, want 7", remaining)
	}
	for _, item := range items {
		result, found, err := f.stream.Result(ctx, item.UserID, f.couponID, item.IdempotencyKey)
		if err != nil || !found || result.Status != "ok" {
			t.Fatalf("fallback result for %s = %+v found=%v err=%v", item.UserID, result, found, err)
		}
	}
	if pending := f.client.XPending(ctx, f.stream.Stream(), f.stream.Group()).Val(); pending == nil || pending.Count != 0 {
		t.Fatalf("pending after fallback = %+v", pending)
	}
}

func TestAsyncClaimerPendingThenRecovers(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	if err := f.stream.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	store := newFakeClaimStore()
	claimer := promotionapp.NewAsyncClaimer(store, f.stream, 80*time.Millisecond)
	worker := promotionapp.NewClaimWorker(store, f.stream, f.stream, "test-consumer", 10, time.Second, time.Minute)

	_, handled, err := claimer.Claim(ctx, "user-a", f.couponID, "idem-a")
	if !handled || !errors.Is(err, coupon.ErrPending) {
		t.Fatalf("first claim handled=%v err=%v, want pending", handled, err)
	}

	items, err := f.stream.Read(ctx, "test-consumer", 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("read: len=%d err=%v", len(items), err)
	}
	worker.Process(ctx, items[0])

	item, handled, err := claimer.Claim(ctx, "user-a", f.couponID, "idem-a")
	if err != nil || !handled || item == nil {
		t.Fatalf("recovery claim: item=%+v handled=%v err=%v", item, handled, err)
	}
}

func TestAsyncClaimerFallsBackWhenCouponNotCached(t *testing.T) {
	ctx := context.Background()
	f := newAsyncFixture(t)
	store := newFakeClaimStore()
	_, handled, err := promotionapp.NewAsyncClaimer(store, f.stream, 50*time.Millisecond).
		Claim(ctx, "user-a", "not-cached-coupon", "idem-x")
	if handled || err != nil {
		t.Fatalf("missing cache handled=%v err=%v, want fallback", handled, err)
	}
}

func (f *asyncFixture) remaining(t *testing.T) int64 {
	t.Helper()
	value, err := f.client.HGet(context.Background(), f.cache.Key(f.couponID), "remaining").Int64()
	if err != nil {
		t.Fatalf("read remaining: %v", err)
	}
	return value
}
