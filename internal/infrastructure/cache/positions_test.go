package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"cityflow/internal/domain"
	"cityflow/internal/domain/location"

	"github.com/redis/go-redis/v9"
)

func TestRedisPositionsIntegration(t *testing.T) {
	address := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR for a dedicated local Redis")
	}
	r := NewRedis(address, "", 0)
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id := fmt.Sprintf("test-rider-%d", time.Now().UnixNano())
	defer func() {
		r.client.Del(context.Background(), positionKey(id))
		r.client.Unlink(context.Background(), positionTrackKey(id))
		r.client.ZRem(context.Background(), positionGeoKey, id)
		r.client.ZRem(context.Background(), positionSeenKey, id)
	}()
	p := location.Position{RiderID: id, Point: location.Point{Latitude: 31.2, Longitude: 121.4}, Accuracy: 10, ObservedAt: time.Now().UTC()}
	saved, err := r.SavePosition(ctx, p, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version <= 0 {
		t.Fatal("missing server version")
	}
	_, err = r.SavePosition(ctx, p, 3*time.Second)
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("rate limit: %v", err)
	}
	_, err = r.SavePosition(ctx, p, 0)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("old position accepted: %v", err)
	}
	values, err := r.NearbyPositions(ctx, p.Point, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range values {
		if v.RiderID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("GEO query missed saved rider")
	}
	loaded, err := r.GetPosition(ctx, id)
	if err != nil || loaded.Version != saved.Version {
		t.Fatalf("snapshot: %+v %v", loaded, err)
	}
	track, err := r.RiderTrack(ctx, id, time.Time{}, time.Time{}, 0, 100)
	if err != nil || len(track) != 1 || track[0].RiderID != id {
		t.Fatalf("track: %+v %v", track, err)
	}
	if err := r.client.PExpire(ctx, positionKey(id), time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	_, err = r.GetPosition(ctx, id)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired snapshot: %v", err)
	}
}

func TestRedisPositionJanitorRemovesOnlyStaleMembers(t *testing.T) {
	address := os.Getenv("CITYFLOW_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CITYFLOW_TEST_REDIS_ADDR for a dedicated local Redis")
	}
	r := NewRedis(address, "", 0)
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	riderID := fmt.Sprintf("test-stale-rider-%d", time.Now().UnixNano())
	defer func() {
		r.client.Unlink(context.Background(), positionTrackKey(riderID))
		r.client.ZRem(context.Background(), positionGeoKey, riderID)
		r.client.ZRem(context.Background(), positionSeenKey, riderID)
	}()
	staleAt := time.Now().Add(-3 * time.Minute).UnixMilli()
	if err := r.client.ZAdd(ctx, positionSeenKey, redis.Z{Score: float64(staleAt), Member: riderID}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := r.client.GeoAdd(ctx, positionGeoKey, &redis.GeoLocation{Name: riderID, Latitude: 31.2, Longitude: 121.4}).Err(); err != nil {
		t.Fatal(err)
	}
	n, err := r.CleanupStalePositions(ctx, 100)
	if err != nil || n < 1 {
		t.Fatalf("cleanup=%d %v", n, err)
	}
	if score, err := r.client.ZScore(ctx, positionSeenKey, riderID).Result(); err == nil && score != 0 {
		t.Fatalf("stale rider still in seen: %v", score)
	}
	if _, err := r.client.ZScore(ctx, positionGeoKey, riderID).Result(); err == nil {
		t.Fatal("stale rider still in GEO")
	}
}
