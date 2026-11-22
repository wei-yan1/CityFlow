package redis

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	reactionservice "cityflow/app/reaction/application/reaction"
)

// TestStoreLifecycle runs against a real Redis. Set CITYFLOW_TEST_REDIS_ADDR
// (for example 127.0.0.1:6380) to enable it; otherwise it is skipped. The test
// uses DB 15 by default (override with CITYFLOW_TEST_REDIS_DB) and refuses DB 0
// so a local run can never flush production reaction keys.
func TestStoreLifecycle(t *testing.T) {
	addr := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("CITYFLOW_TEST_REDIS_ADDR is not set")
	}
	testDB := 15
	if raw := os.Getenv("CITYFLOW_TEST_REDIS_DB"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("invalid CITYFLOW_TEST_REDIS_DB: %v", err)
		}
		testDB = parsed
	}
	if testDB == 0 {
		t.Skip("CITYFLOW_TEST_REDIS_DB must be nonzero so tests never flush production DB 0")
	}
	client := goredis.NewClient(&goredis.Options{Addr: addr, DB: testDB})
	t.Cleanup(func() {
		_ = client.FlushDB(context.Background()).Err()
		_ = client.Close()
	})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("test redis unavailable: %v", err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UTC().UnixNano()
	target := fmt.Sprintf("reaction-test-%d", suffix)
	// Keep the real target type so the Lua projection path runs; cleanup only
	// removes this member and never deletes the shared production ZSET.
	targetType := "PLACE_REVIEW"
	userA := fmt.Sprintf("user-a-%d", suffix)
	userB := fmt.Sprintf("user-b-%d", suffix)
	keys := []string{
		likeSetPrefix + target,
		favoriteSetPrefix + target,
		userIndexPrefix + userA + ":LIKE:" + targetType,
		userIndexPrefix + userA + ":FAVORITE:" + targetType,
		userIndexPrefix + userB + ":LIKE:" + targetType,
	}
	t.Cleanup(func() {
		_ = client.ZRem(ctx, timesKey(targetType), target).Err()
		_ = client.Del(ctx, keys...).Err()
	})
	_ = client.ZRem(ctx, timesKey(targetType), target).Err()
	_ = client.Del(ctx, keys...).Err()

	store := New(client)
	active, count, err := store.Set(ctx, targetType, target, userA, "LIKE", true, time.Now().UTC())
	if err != nil || !active || count != 1 {
		t.Fatalf("first like = %v %d %v", active, count, err)
	}
	if _, count, err = store.Set(ctx, targetType, target, userB, "LIKE", true, time.Now().UTC()); err != nil || count != 2 {
		t.Fatalf("second like = %d %v", count, err)
	}
	if _, count, err = store.Set(ctx, targetType, target, userA, "LIKE", true, time.Now().UTC()); err != nil || count != 2 {
		t.Fatalf("idempotent like = %d %v", count, err)
	}
	if score, err := client.ZScore(ctx, timesKey(targetType), target).Result(); err != nil || score != 2 {
		t.Fatalf("times score = %v %v", score, err)
	}

	states, err := store.States(ctx, targetType, []string{target}, userA, "LIKE")
	if err != nil || len(states) != 1 || states[0].Count != 2 || !states[0].Active {
		t.Fatalf("states = %+v %v", states, err)
	}
	anonymous, err := store.States(ctx, targetType, []string{target}, "", "LIKE")
	if err != nil || len(anonymous) != 1 || anonymous[0].Active {
		t.Fatalf("anonymous states = %+v %v", anonymous, err)
	}

	mine, next, err := store.ListMine(ctx, userA, "LIKE", targetType, "", 10)
	if err != nil || len(mine) != 1 || mine[0].TargetID != target || next != "" {
		t.Fatalf("mine = %+v next=%q err=%v", mine, next, err)
	}

	pending, err := store.PopPending(ctx, targetType, 30)
	if err != nil || len(pending) != 1 || pending[0].TargetID != target || pending[0].Count != 2 {
		t.Fatalf("pending = %+v %v", pending, err)
	}

	// Requeue must not overwrite a newer absolute count.
	if err := client.ZAdd(ctx, timesKey(targetType), goredis.Z{Score: 5, Member: target}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := store.RequeuePending(ctx, targetType, []reactionservice.Pending{{TargetID: target, Count: 2}}); err != nil {
		t.Fatal(err)
	}
	if score, _ := client.ZScore(ctx, timesKey(targetType), target).Result(); score != 5 {
		t.Fatalf("requeue overwrote newer score: %v", score)
	}

	if _, count, err = store.Set(ctx, targetType, target, userA, "LIKE", false, time.Now().UTC()); err != nil || count != 1 {
		t.Fatalf("unlike A = %d %v", count, err)
	}
	if _, count, err = store.Set(ctx, targetType, target, userA, "LIKE", false, time.Now().UTC()); err != nil || count != 1 {
		t.Fatalf("idempotent unlike A = %d %v", count, err)
	}
	if score, _ := client.ZScore(ctx, timesKey(targetType), target).Result(); score != 1 {
		t.Fatalf("times after unlike = %v", score)
	}
	if members, _ := client.SMembers(ctx, likeSetPrefix+target).Result(); len(members) != 1 || members[0] != userB {
		t.Fatalf("remaining members = %v", members)
	}

	if _, count, err = store.Set(ctx, targetType, target, userB, "LIKE", false, time.Now().UTC()); err != nil || count != 0 {
		t.Fatalf("unlike B = %d %v", count, err)
	}
	if score, _ := client.ZScore(ctx, timesKey(targetType), target).Result(); score != 0 {
		t.Fatalf("times after last unlike = %v", score)
	}
	if mine, _, err = store.ListMine(ctx, userA, "LIKE", targetType, "", 10); err != nil || len(mine) != 0 {
		t.Fatalf("mine after unlike = %+v %v", mine, err)
	}

	// Favorites share the state pipeline but never enter the like projection.
	if _, count, err = store.Set(ctx, targetType, target, userA, "FAVORITE", true, time.Now().UTC()); err != nil || count != 1 {
		t.Fatalf("favorite = %d %v", count, err)
	}
	if states, err = store.States(ctx, targetType, []string{target}, userA, "FAVORITE"); err != nil || len(states) != 1 || states[0].Count != 1 || !states[0].Active {
		t.Fatalf("favorite states = %+v %v", states, err)
	}
	if mine, _, err = store.ListMine(ctx, userA, "FAVORITE", targetType, "", 10); err != nil || len(mine) != 1 || mine[0].TargetID != target {
		t.Fatalf("favorite mine = %+v %v", mine, err)
	}
	if exists, _ := client.Exists(ctx, timesKey(targetType)).Result(); exists != 1 {
		t.Fatalf("favorite should not remove like projection key")
	}
}
