package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMapColdRequestCoalescingAndCancellation(t *testing.T) {
	m := NewMapCache(nil, 0, 60)
	defer m.Close()
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	load := func(ctx context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return []byte(`{"ok":true}`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := m.Load(ctx, "same-city", MapPolicy{}, load); first <- err }()
	<-started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first cancellation: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := m.Load(context.Background(), "same-city", MapPolicy{}, load)
			if err != nil || string(body) != `{"ok":true}` {
				t.Errorf("shared request: %s %v", body, err)
			}
		}()
	}
	// Keep the provider pending while every waiter enters singleflight.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("provider calls=%d; want 1", calls.Load())
	}
}

func TestMapLocalQuotaAndFailures(t *testing.T) {
	m := NewMapCache(nil, 0, 1)
	defer m.Close()
	if err := m.Allow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Allow(context.Background()); !errors.Is(err, ErrMapQuota) {
		t.Fatalf("quota: %v", err)
	}
	var calls int
	for i := 0; i < 2; i++ {
		_, err := m.Load(context.Background(), "fail", MapPolicy{Force: true, TTL: time.Hour}, func(context.Context) ([]byte, error) { calls++; return nil, errors.New("provider unavailable") })
		if err == nil {
			t.Fatal("error cached as success")
		}
	}
	if calls != 2 {
		t.Fatalf("failure calls=%d", calls)
	}
}

func testMapRedis(t *testing.T) (*MapCache, *Redis) {
	t.Helper()
	address := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR for real Redis map checks")
	}
	r := NewRedis(address, "", 0)
	if err := r.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := NewMapCache(r, 128<<20, 60)
	m.prefix = fmt.Sprintf("cityflow:{map-test-%d}:v1:", time.Now().UnixNano())
	t.Cleanup(func() {
		m.Close()
		ctx := context.Background()
		keys, _ := r.client.Keys(ctx, m.prefix+"*").Result()
		if len(keys) > 0 {
			_ = r.client.Del(ctx, keys...).Err()
		}
		r.Close()
	})
	return m, r
}

func TestMapRedisAdmissionSWRAndGeneration(t *testing.T) {
	m, r := testMapRedis(t)
	ctx := context.Background()
	var calls int
	load := func(context.Context) ([]byte, error) { calls++; return []byte(`{"version":1}`), nil }
	p := MapPolicy{TTL: time.Minute}
	for i := 0; i < 22; i++ {
		if _, err := m.Load(ctx, "hot", p, load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 21 {
		t.Fatalf("threshold calls=%d; want 21", calls)
	}
	gen, err := m.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Invalidate(ctx, "hot"); err != nil {
		t.Fatal(err)
	}
	// A pre-invalidation loader cannot repopulate the old generation.
	_, err = m.Load(ctx, "hot", MapPolicy{TTL: time.Minute, Force: true, Generation: gen}, load)
	if err != nil {
		t.Fatal(err)
	}
	if r.client.HExists(ctx, m.keys()[0], mapDigest("hot")).Val() {
		t.Fatal("old generation repopulated cache")
	}
	current, _ := m.Generation(ctx)
	envelope, _ := json.Marshal(mapEnvelope{Body: json.RawMessage(`{"version":0}`), Fresh: time.Now().Add(-time.Second).UnixMilli()})
	r.client.Eval(ctx, mapStoreLua, append(m.keys(), m.prefix+"generation"), mapDigest("stale"), string(envelope), time.Now().UnixMilli(), time.Now().Add(time.Minute).UnixMilli(), current, m.budget, m.prefix+"entry:")
	started, release := make(chan struct{}), make(chan struct{})
	body, err := m.Load(ctx, "stale", MapPolicy{TTL: time.Minute, Stale: time.Minute, Force: true}, func(context.Context) ([]byte, error) { close(started); <-release; return []byte(`{"version":2}`), nil })
	if err != nil || string(body) != `{"version":0}` {
		t.Fatalf("SWR: %s %v", body, err)
	}
	<-started
	close(release)
}

func TestMapRedisBudgetAndLRU(t *testing.T) {
	m, r := testMapRedis(t)
	m.budget = 65536
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		_, err := m.Load(ctx, fmt.Sprint(i), MapPolicy{Force: true, TTL: time.Hour}, func(context.Context) ([]byte, error) {
			v := make([]byte, 8192)
			for i := range v {
				v[i] = 'a'
			}
			return json.Marshal(string(v))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var memory int64
	for _, key := range m.keys() {
		memory += r.client.MemoryUsage(ctx, key).Val()
	}
	payloadKeys, _ := r.client.Keys(ctx, m.prefix+"entry:*").Result()
	for _, key := range payloadKeys {
		memory += r.client.MemoryUsage(ctx, key).Val()
	}
	if memory > m.budget {
		t.Fatalf("map budget %d > %d", memory, m.budget)
	}
	if r.client.HExists(ctx, m.keys()[0], mapDigest("0")).Val() {
		t.Fatal("old entry was not evicted")
	}
	if !r.client.HExists(ctx, m.keys()[0], mapDigest("39")).Val() {
		t.Fatal("recent entry missing")
	}
}

func TestMapRedisPayloadExpiresWithoutSubsequentAccess(t *testing.T) {
	m, r := testMapRedis(t)
	ctx := context.Background()
	_, err := m.Load(ctx, "short-lived", MapPolicy{Force: true, TTL: 30 * time.Millisecond}, func(context.Context) ([]byte, error) { return []byte(`{"temporary":true}`), nil })
	if err != nil {
		t.Fatal(err)
	}
	key := m.prefix + "entry:" + mapDigest("short-lived")
	if ttl := r.client.PTTL(ctx, key).Val(); ttl <= 0 || ttl > 30*time.Millisecond {
		t.Fatalf("native expiry=%v", ttl)
	}
	time.Sleep(60 * time.Millisecond)
	if r.client.Exists(ctx, key).Val() != 0 {
		t.Fatal("expired provider payload remained readable without another map request")
	}
}
