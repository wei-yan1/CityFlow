package trackingredis

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"cityflow/internal/common/location"
	"cityflow/internal/platform/cache"
)

// TestStoreQueuesTrackForArchive runs against a real Redis. It is skipped
// unless CITYFLOW_TEST_REDIS_ADDR is set. The default DB is 14 so this test
// does not share DB 15 with the existing Reaction/Promotion integration tests.
func TestStoreQueuesTrackForArchive(t *testing.T) {
	addr := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("CITYFLOW_TEST_REDIS_ADDR is not set")
	}
	db := 14
	if raw := os.Getenv("CITYFLOW_TRACKING_TEST_REDIS_DB"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("invalid CITYFLOW_TRACKING_TEST_REDIS_DB: %v", err)
		}
		db = parsed
	}
	if db == 0 {
		t.Skip("tracking test must not use DB 0")
	}

	redis := cache.NewRedis(addr, "", db)
	t.Cleanup(func() { _ = redis.Close() })
	ctx := context.Background()
	if err := redis.Ping(ctx); err != nil {
		t.Skipf("test redis unavailable: %v", err)
	}
	store := New(redis)
	riderID := fmt.Sprintf("tracking-test-%d", time.Now().UTC().UnixNano())
	legacyRiderID := riderID + "-legacy"
	t.Cleanup(func() {
		_ = redis.Client().Del(ctx, positionKey(riderID), positionTrackKey(riderID), positionTrackKey(legacyRiderID)).Err()
		_ = redis.Client().ZRem(ctx, positionGeoKey, riderID).Err()
		_ = redis.Client().ZRem(ctx, positionSeenKey, riderID).Err()
		_ = redis.Client().SRem(ctx, trackRidersKey, riderID, legacyRiderID).Err()
		_ = redis.Client().HDel(ctx, trackCursorsKey, riderID).Err()
	})

	base := time.Now().UTC().Add(-time.Hour)
	for index := 0; index < 401; index++ {
		if _, err := store.SavePosition(ctx, location.Position{
			RiderID:    riderID,
			Point:      location.Point{Latitude: 25.273, Longitude: 110.29},
			Accuracy:   10,
			ObservedAt: base.Add(time.Duration(index) * time.Second),
		}, 0); err != nil {
			t.Fatalf("save point %d: %v", index, err)
		}
	}

	ttl, err := redis.Client().TTL(ctx, positionTrackKey(riderID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl != -1 {
		t.Fatalf("track TTL=%v, want -1 (no expiry before archive)", ttl)
	}
	count, err := redis.Client().ZCard(ctx, positionTrackKey(riderID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if count != 401 {
		t.Fatalf("track has %d points, want 401 pending", count)
	}
	queued, err := redis.Client().SIsMember(ctx, trackRidersKey, riderID).Result()
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("rider was not added to the pending rider set")
	}

	// A legacy ZSET has no rider-set entry. The one-time backfill must register
	// it so the archiver can find and drain it after the upgrade.
	if err := redis.Client().ZAdd(ctx, positionTrackKey(legacyRiderID), goredis.Z{
		Score: float64(time.Now().UTC().UnixMilli()), Member: `{"rider_id":"` + legacyRiderID + `"}`,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := store.BackfillPendingRiders(ctx); err != nil {
		t.Fatal(err)
	}
	queued, err = redis.Client().SIsMember(ctx, trackRidersKey, legacyRiderID).Result()
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("legacy pending rider was not registered by backfill")
	}

	batch, err := store.PendingTrackBatch(ctx, riderID, 0, 401)
	if err != nil {
		t.Fatalf("read pending batch: %v", err)
	}
	if len(batch.Points) != 401 || len(batch.Members) != 401 {
		t.Fatalf("batch points=%d members=%d, want 401 each", len(batch.Points), len(batch.Members))
	}
	if batch.LastVersion == 0 {
		t.Fatal("batch cursor was not derived from point versions")
	}
	if err := store.SetTrackCursor(ctx, riderID, batch.LastVersion); err != nil {
		t.Fatal(err)
	}
	cursor, err := store.TrackCursor(ctx, riderID)
	if err != nil {
		t.Fatal(err)
	}
	if cursor != batch.LastVersion {
		t.Fatalf("cursor=%d want=%d", cursor, batch.LastVersion)
	}
	after, err := store.PendingTrackBatch(ctx, riderID, cursor, 401)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Points) != 0 {
		t.Fatalf("batch after cursor has %d points, want 0", len(after.Points))
	}

	if err := store.RemoveTrackMembers(ctx, riderID, batch.Members[:10]); err != nil {
		t.Fatal(err)
	}
	count, err = redis.Client().ZCard(ctx, positionTrackKey(riderID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if count != 391 {
		t.Fatalf("track has %d points after removing 10, want 391", count)
	}
	forgotten, err := store.ForgetRiderIfEmpty(ctx, riderID)
	if err != nil {
		t.Fatal(err)
	}
	if forgotten {
		t.Fatal("rider forgotten while pending points remain")
	}

	if err := store.RemoveTrackMembers(ctx, riderID, batch.Members[10:]); err != nil {
		t.Fatal(err)
	}
	forgotten, err = store.ForgetRiderIfEmpty(ctx, riderID)
	if err != nil {
		t.Fatal(err)
	}
	if !forgotten {
		t.Fatal("empty rider queue was not forgotten")
	}
	queued, err = redis.Client().SIsMember(ctx, trackRidersKey, riderID).Result()
	if err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Fatal("rider remained in the pending rider set")
	}
	cursor, err = store.TrackCursor(ctx, riderID)
	if err != nil {
		t.Fatal(err)
	}
	if cursor != 0 {
		t.Fatalf("cursor=%d want 0 after forgetting rider", cursor)
	}

	// A key written by the previous TTL implementation must lose its TTL on
	// the next report, otherwise an existing pending queue would expire.
	if _, err := store.SavePosition(ctx, location.Position{
		RiderID:    riderID,
		Point:      location.Point{Latitude: 25.273, Longitude: 110.29},
		Accuracy:   10,
		ObservedAt: base.Add(402 * time.Second),
	}, 0); err != nil {
		t.Fatalf("save after legacy TTL: %v", err)
	}
	if err := redis.Client().PExpire(ctx, positionTrackKey(riderID), time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePosition(ctx, location.Position{
		RiderID:    riderID,
		Point:      location.Point{Latitude: 25.273, Longitude: 110.29},
		Accuracy:   10,
		ObservedAt: base.Add(403 * time.Second),
	}, 0); err != nil {
		t.Fatalf("save after PExpire: %v", err)
	}
	ttl, err = redis.Client().TTL(ctx, positionTrackKey(riderID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl != -1 {
		t.Fatalf("track TTL after PERSIST=%v, want -1", ttl)
	}
}
