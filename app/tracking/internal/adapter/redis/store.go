package trackingredis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"cityflow/app/tracking/repository"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/cache"
)

// Store owns the rider position, GEO and trajectory read/write model. The
// generic Redis connection is injected from internal/platform/cache.
type Store struct{ *cache.Redis }

func New(redis *cache.Redis) *Store { return &Store{Redis: redis} }

const positionGeoKey = "cityflow:{positions}:geo"
const positionSeenKey = "cityflow:{positions}:seen"
const positionJanitorLockKey = "cityflow:{positions}:janitor:lock"
const trackRidersKey = "cityflow:{positions}:track:riders"
const trackCursorsKey = "cityflow:{positions}:track:cursors"
const trackArchiverLockKey = "cityflow:{positions}:track:archiver:lock"

func positionKey(id string) string { return "cityflow:{positions}:rider:" + id }
func positionTrackKey(id string) string {
	return "cityflow:{positions}:track:rider:" + id
}

// One script enforces rate/order, updates the snapshot and GEO index atomically,
// and appends a rider-centric trajectory sample to the pending queue. The
// pending queue has no TTL; the MQ archive consumer removes each member only
// after the point is committed to MySQL. PERSIST clears a TTL written by an
// older deployment when an existing key receives its next point.
// GEO/seen cleanup is handled by the janitor, not by every report.
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
redis.call('PERSIST',KEYS[4])
redis.call('SADD',KEYS[5],ARGV[2])
return {now,encoded}
`)

func (r *Store) SavePosition(ctx context.Context, p location.Position, interval time.Duration) (*location.Position, error) {
	p.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	values, err := savePosition.Run(ctx, r.Client(), []string{positionKey(p.RiderID), positionGeoKey, positionSeenKey, positionTrackKey(p.RiderID), trackRidersKey}, string(body), p.RiderID, interval.Milliseconds(), p.ObservedAt.UnixMilli()).Slice()
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
func (r *Store) GetPosition(ctx context.Context, id string) (*location.Position, error) {
	body, err := r.Client().Get(ctx, positionKey(id)).Bytes()
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
func (r *Store) NearbyPositions(ctx context.Context, center location.Point, radius float64, limit int) ([]location.Position, error) {
	if limit <= 0 {
		limit = 50
	}
	// Expired GEO members may occupy the candidate window until the janitor
	// removes them; keep enough candidates to still fill a page of live riders.
	count := limit * 10
	if count < 500 {
		count = 500
	}
	ids, err := r.Client().GeoSearch(ctx, positionGeoKey, &redis.GeoSearchQuery{Longitude: center.Longitude, Latitude: center.Latitude, Radius: radius, RadiusUnit: "m", Sort: "ASC", Count: count}).Result()
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
func (r *Store) RiderTrack(ctx context.Context, riderID string, from, to time.Time, after uint64, limit int) ([]location.TrackPoint, error) {
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
	values, err := r.Client().ZRangeByScore(ctx, positionTrackKey(riderID), &redis.ZRangeBy{Min: min, Max: max, Offset: 0, Count: int64(limit)}).Result()
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

func (r *Store) DeleteRiderTrack(ctx context.Context, riderID string) error {
	return r.Client().Unlink(ctx, positionTrackKey(riderID)).Err()
}

// PendingRiders returns the rider IDs that have ever queued a trajectory
// sample. The archiver clears riders atomically once their ZSET is empty.
func (r *Store) PendingRiders(ctx context.Context) ([]string, error) {
	riders, err := r.Client().SMembers(ctx, trackRidersKey).Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(riders)
	return riders, nil
}

// PendingTrackBatch reads samples after the persisted cursor. It does not
// remove them: the MQ consumer deletes only after the MySQL transaction is
// committed, so a failed publish or crash leaves the batch retryable.
// BackfillPendingRiders registers legacy track keys that predate the pending
// rider set. Without this one-time pass, a rider who has not reported since the
// upgrade would keep an unarchived ZSET forever because the archiver only
// enumerates members of track:riders.
func (r *Store) BackfillPendingRiders(ctx context.Context) error {
	prefix := positionTrackKey("")
	var cursor uint64
	for {
		keys, next, err := r.Client().Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		riders := make([]string, 0, len(keys))
		for _, key := range keys {
			riderID := strings.TrimPrefix(key, prefix)
			if riderID != "" && riderID != key {
				riders = append(riders, riderID)
			}
		}
		if len(riders) > 0 {
			if err := r.Client().SAdd(ctx, trackRidersKey, riders).Err(); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

func (r *Store) PendingTrackBatch(ctx context.Context, riderID string, after uint64, limit int) (repository.TrackBatch, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	min := "-inf"
	if after > 0 {
		min = "(" + strconv.FormatUint(after, 10)
	}
	values, err := r.Client().ZRangeByScore(ctx, positionTrackKey(riderID), &redis.ZRangeBy{Min: min, Max: "+inf", Offset: 0, Count: int64(limit)}).Result()
	if err != nil {
		return repository.TrackBatch{}, err
	}
	batch := repository.TrackBatch{RiderID: riderID, Points: make([]location.TrackPoint, 0, len(values)), Members: make([]string, 0, len(values))}
	for _, raw := range values {
		var p location.Position
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return repository.TrackBatch{}, err
		}
		if p.RiderID == "" {
			p.RiderID = riderID
		}
		id := uint64(p.Version)
		if p.Version <= 0 {
			id = uint64(p.ObservedAt.UnixMilli())
		}
		batch.Points = append(batch.Points, location.TrackPoint{ID: id, TaskID: p.TaskID, Position: p})
		batch.Members = append(batch.Members, raw)
		if id > batch.LastVersion {
			batch.LastVersion = id
		}
	}
	return batch, nil
}

func (r *Store) TrackCursor(ctx context.Context, riderID string) (uint64, error) {
	value, err := r.Client().HGet(ctx, trackCursorsKey, riderID).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(value, 10, 64)
}

func (r *Store) SetTrackCursor(ctx context.Context, riderID string, version uint64) error {
	return r.Client().HSet(ctx, trackCursorsKey, riderID, strconv.FormatUint(version, 10)).Err()
}

func (r *Store) RemoveTrackMembers(ctx context.Context, riderID string, members []string) error {
	if len(members) == 0 {
		return nil
	}
	args := make([]any, 0, len(members))
	for _, member := range members {
		args = append(args, member)
	}
	return r.Client().ZRem(ctx, positionTrackKey(riderID), args...).Err()
}

var forgetRiderIfEmpty = redis.NewScript(`
if redis.call('ZCARD',KEYS[1])==0 then
 redis.call('DEL',KEYS[1])
 redis.call('SREM',KEYS[2],ARGV[1])
 redis.call('HDEL',KEYS[3],ARGV[1])
 return 1
end
return 0
`)

func (r *Store) ForgetRiderIfEmpty(ctx context.Context, riderID string) (bool, error) {
	return forgetRiderIfEmpty.Run(ctx, r.Client(), []string{positionTrackKey(riderID), trackRidersKey, trackCursorsKey}, riderID).Bool()
}

func (r *Store) AcquireArchiverLock(ctx context.Context, ttl time.Duration) (string, bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	token, err := newLockToken()
	if err != nil {
		return "", false, err
	}
	acquired, err := r.Client().SetNX(ctx, trackArchiverLockKey, token, ttl).Result()
	if err != nil {
		return "", false, err
	}
	if !acquired {
		return "", false, nil
	}
	return token, true, nil
}

func (r *Store) ReleaseArchiverLock(ctx context.Context, token string) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return releaseTokenLock.Run(releaseCtx, r.Client(), []string{trackArchiverLockKey}, token).Err()
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

var releaseTokenLock = redis.NewScript(`
if redis.call('GET',KEYS[1])==ARGV[1] then
 return redis.call('DEL',KEYS[1])
end
return 0
`)

// CleanupStalePositions removes expired GEO/seen members. It is intentionally
// separate from SavePosition so a report never pays for global cleanup. A
// short Redis token lock keeps every Tracking instance from scanning the same
// ZSET at the same time without tying cleanup availability to the scheduler.
func (r *Store) CleanupStalePositions(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	token, err := newLockToken()
	if err != nil {
		return 0, err
	}
	acquired, err := r.Client().SetNX(ctx, positionJanitorLockKey, token, 30*time.Second).Result()
	if err != nil {
		return 0, err
	}
	if !acquired {
		return 0, nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = releaseTokenLock.Run(releaseCtx, r.Client(), []string{positionJanitorLockKey}, token).Err()
	}()
	n, err := cleanupStalePositions.Run(ctx, r.Client(), []string{positionSeenKey, positionGeoKey}, limit).Int()
	if err != nil {
		return 0, err
	}
	return n, nil
}

func newLockToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
