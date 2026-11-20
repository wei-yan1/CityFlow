package cache

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
)

const positionGeoKey = "cityflow:{positions}:geo"
const positionSeenKey = "cityflow:{positions}:seen"

func positionKey(id string) string { return "cityflow:{positions}:rider:" + id }
func positionTrackKey(id string) string {
	return "cityflow:{positions}:track:rider:" + id
}

// One script enforces rate/order, updates the snapshot and GEO index atomically,
// and appends a rider-centric trajectory sample. GEO/ZSET cleanup is handled by
// the janitor, not by every report.
var savePosition = redis.NewScript(`
local t=redis.call('TIME')
local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local previous=redis.call('GET',KEYS[1])
if previous then
 local old=cjson.decode(previous)
 if now-tonumber(old.version)<tonumber(ARGV[3]) then return {-1,''} end
 if tonumber(ARGV[4])<=tonumber(old.observed_ms) then return {-2,''} end
end
local value=cjson.decode(ARGV[1])
value.version=now
value.observed_ms=tonumber(ARGV[4])
local encoded=cjson.encode(value)
redis.call('SET',KEYS[1],encoded,'PX',120000)
redis.call('GEOADD',KEYS[2],value.longitude,value.latitude,ARGV[2])
redis.call('ZADD',KEYS[3],now,ARGV[2])
redis.call('ZADD',KEYS[4],now,encoded)
redis.call('PEXPIRE',KEYS[4],3600000)
local trackCount=redis.call('ZCARD',KEYS[4])
if trackCount>400 then
 redis.call('ZREMRANGEBYSCORE',KEYS[4],'-inf',now-3600000)
end
return {now,encoded}
`)

func (r *Redis) SavePosition(ctx context.Context, p location.Position, interval time.Duration) (*location.Position, error) {
	p.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	values, err := savePosition.Run(ctx, r.client, []string{positionKey(p.RiderID), positionGeoKey, positionSeenKey, positionTrackKey(p.RiderID)}, string(body), p.RiderID, interval.Milliseconds(), p.ObservedAt.UnixMilli()).Slice()
	if err != nil {
		return nil, err
	}
	code := values[0].(int64)
	if code == -1 {
		return nil, domain.ErrRateLimited
	}
	if code == -2 {
		return nil, domain.ErrConflict
	}
	var result location.Position
	if err := json.Unmarshal([]byte(values[1].(string)), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
func (r *Redis) GetPosition(ctx context.Context, id string) (*location.Position, error) {
	body, err := r.client.Get(ctx, positionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var p location.Position
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
func (r *Redis) NearbyPositions(ctx context.Context, center location.Point, radius float64, limit int) ([]location.Position, error) {
	if limit <= 0 {
		limit = 50
	}
	// Expired GEO members may occupy the candidate window until the janitor
	// removes them; keep enough candidates to still fill a page of live riders.
	count := limit * 10
	if count < 500 {
		count = 500
	}
	ids, err := r.client.GeoSearch(ctx, positionGeoKey, &redis.GeoSearchQuery{Longitude: center.Longitude, Latitude: center.Latitude, Radius: radius, RadiusUnit: "m", Sort: "ASC", Count: count}).Result()
	if err != nil {
		return nil, err
	}
	result := make([]location.Position, 0)
	for _, id := range ids {
		p, err := r.GetPosition(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, *p)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

// RiderTrack returns rider-centric samples in [from, to]. The caller resolves
// task ownership into one or more rider/time windows. after is a server
// version/id cursor and is exclusive.
func (r *Redis) RiderTrack(ctx context.Context, riderID string, from, to time.Time, after uint64, limit int) ([]location.TrackPoint, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 501 {
		limit = 501
	}
	min := "-inf"
	if !from.IsZero() {
		min = strconv.FormatInt(from.UnixMilli(), 10)
	}
	max := "+inf"
	if !to.IsZero() {
		max = strconv.FormatInt(to.UnixMilli(), 10)
	}
	if after > 0 {
		min = "(" + strconv.FormatUint(after, 10)
	}
	values, err := r.client.ZRangeByScore(ctx, positionTrackKey(riderID), &redis.ZRangeBy{Min: min, Max: max, Offset: 0, Count: int64(limit)}).Result()
	if err != nil {
		return nil, err
	}
	result := make([]location.TrackPoint, 0, len(values))
	for _, value := range values {
		var p location.Position
		if err := json.Unmarshal([]byte(value), &p); err != nil {
			return nil, err
		}
		id := uint64(p.Version)
		if id == 0 {
			id = uint64(p.ObservedAt.UnixMilli())
		}
		result = append(result, location.TrackPoint{ID: id, TaskID: p.TaskID, Position: p})
	}
	return result, nil
}

func (r *Redis) DeleteRiderTrack(ctx context.Context, riderID string) error {
	return r.client.Unlink(ctx, positionTrackKey(riderID)).Err()
}

var cleanupStalePositions = redis.NewScript(`
local t=redis.call('TIME')
local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
local cutoff=now-120000
local limit=tonumber(ARGV[1])
local stale=redis.call('ZRANGEBYSCORE',KEYS[1],'-inf',cutoff,'LIMIT',0,limit,'WITHSCORES')
local removed=0
for i=1,#stale,2 do
 redis.call('ZREM',KEYS[2],stale[i])
 redis.call('ZREM',KEYS[1],stale[i])
 removed=removed+1
end
return removed
`)

// CleanupStalePositions removes expired GEO/seen members. It is intentionally
// separate from SavePosition so a report never pays for global cleanup.
func (r *Redis) CleanupStalePositions(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	n, err := cleanupStalePositions.Run(ctx, r.client, []string{positionSeenKey, positionGeoKey}, limit).Int()
	if err != nil {
		return 0, err
	}
	return n, nil
}
