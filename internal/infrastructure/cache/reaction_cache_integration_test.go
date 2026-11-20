package cache

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"cityflow/internal/domain/reaction"
	"github.com/redis/go-redis/v9"
)

// TestReactionCacheRoundTrip exercises the real Lua path, the projection
// queue and the ops stream. It is skipped unless a disposable Redis is
// provided; the test flushes DB 15 only, never DB 0.
func TestReactionCacheRoundTrip(t *testing.T) {
	addr := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR to run Redis integration")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis unavailable: %v", err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	defer client.FlushDB(ctx)

	cache := NewReactionCache(client)
	changed, count, err := cache.Set(ctx, reaction.TargetPlaceReview, "review-1", "user-1", reaction.KindLike, true)
	if err != nil || !changed || count != 1 {
		t.Fatalf("first set changed=%v count=%d err=%v", changed, count, err)
	}
	changed, count, err = cache.Set(ctx, reaction.TargetPlaceReview, "review-1", "user-1", reaction.KindLike, true)
	if err != nil || changed || count != 1 {
		t.Fatalf("idempotent set changed=%v count=%d err=%v", changed, count, err)
	}

	states, err := cache.GetStates(ctx, reaction.TargetPlaceReview, []string{"review-1"}, "user-1", reaction.KindLike)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states[0].CountKnown || states[0].Count != 1 || !states[0].Liked {
		t.Fatalf("state = %+v", states)
	}

	// PopProjection must not consume the display counter.
	projections, err := cache.PopProjection(ctx, reaction.TargetPlaceReview, 10)
	if err != nil || len(projections) != 1 || projections[0].Count != 1 {
		t.Fatalf("projection = %+v err=%v", projections, err)
	}
	states, err = cache.GetStates(ctx, reaction.TargetPlaceReview, []string{"review-1"}, "user-1", reaction.KindLike)
	if err != nil || states[0].Count != 1 {
		t.Fatalf("counter after pop = %+v err=%v", states, err)
	}
	if err := cache.RequeueProjection(ctx, reaction.TargetPlaceReview, projections); err != nil {
		t.Fatal(err)
	}
	again, err := cache.PopProjection(ctx, reaction.TargetPlaceReview, 10)
	if err != nil || !reflect.DeepEqual(again, projections) {
		t.Fatalf("requeue = %+v err=%v", again, err)
	}

	changed, count, err = cache.Set(ctx, reaction.TargetPlaceReview, "review-1", "user-1", reaction.KindLike, false)
	if err != nil || !changed || count != 0 {
		t.Fatalf("unlike changed=%v count=%d err=%v", changed, count, err)
	}
	states, err = cache.GetStates(ctx, reaction.TargetPlaceReview, []string{"review-1"}, "user-1", reaction.KindLike)
	if err != nil || !states[0].CountKnown || states[0].Count != 0 || states[0].Liked {
		t.Fatalf("unlike state = %+v err=%v", states, err)
	}
	zero, err := cache.PopProjection(ctx, reaction.TargetPlaceReview, 10)
	if err != nil || len(zero) != 1 || zero[0].Count != 0 {
		t.Fatalf("zero projection = %+v err=%v", zero, err)
	}

	// The ops stream carries exactly the two changed operations.
	messages, err := cache.ReadOps(ctx, "consumer-1", 10, time.Second)
	if err != nil || len(messages) != 2 {
		t.Fatalf("ops = %+v err=%v", messages, err)
	}
	if !messages[0].Active || messages[1].Active {
		t.Fatalf("ops order = %+v", messages)
	}
	if err := cache.AckOps(ctx, messages[0].MessageID, messages[1].MessageID); err != nil {
		t.Fatal(err)
	}

	// Comment likes keep a display counter but must not enter the projection
	// queue: comment tables have no like_count column and no consumer.
	changed, count, err = cache.Set(ctx, reaction.TargetPlaceComment, "comment-1", "user-1", reaction.KindLike, true)
	if err != nil || !changed || count != 1 {
		t.Fatalf("comment like changed=%v count=%d err=%v", changed, count, err)
	}
	projectionCount, err := client.ZCard(ctx, reactionProjectionKey(reaction.TargetPlaceComment)).Result()
	if err != nil || projectionCount != 0 {
		t.Fatalf("comment projection queue = %d err=%v", projectionCount, err)
	}
	displayCount, err := client.ZScore(ctx, reactionCountKey(reaction.TargetPlaceComment), "comment-1").Result()
	if err != nil || displayCount != 1 {
		t.Fatalf("comment display count = %v err=%v", displayCount, err)
	}
}
