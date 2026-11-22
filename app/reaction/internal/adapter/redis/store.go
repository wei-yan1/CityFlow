// Package redis implements the Reaction request-path store. The set key is the
// exact contract from the product design: one set of user ids per biz id plus a
// per-type ZSET holding the latest absolute like count.
package redis

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	reactionservice "cityflow/app/reaction/application/reaction"
	"cityflow/internal/common/contract"
)

const (
	likeSetPrefix     = "likes:set:biz:"
	favoriteSetPrefix = "likes:set:fav:"
	timesPrefix       = "likes:times:type:"
	userIndexPrefix   = "likes:user:"
)

var setScript = goredis.NewScript(`
local changed
if ARGV[2] == '1' then
  changed = redis.call('SADD', KEYS[1], ARGV[1])
else
  changed = redis.call('SREM', KEYS[1], ARGV[1])
end
local count = redis.call('SCARD', KEYS[1])
if changed == 1 then
  if ARGV[4] == 'LIKE' and ARGV[5] == '1' then
    redis.call('ZADD', KEYS[2], count, ARGV[3])
  end
  if ARGV[2] == '1' then
    redis.call('ZADD', KEYS[3], ARGV[6], ARGV[3])
  else
    redis.call('ZREM', KEYS[3], ARGV[3])
  end
end
return {count, changed}
`)

var requeueScript = goredis.NewScript(`
for i = 1, #ARGV, 2 do
  if redis.call('ZSCORE', KEYS[1], ARGV[i]) == false then
    redis.call('ZADD', KEYS[1], ARGV[i + 1], ARGV[i])
  end
end
return 1
`)

type Store struct {
	client *goredis.Client
}

func New(client *goredis.Client) *Store {
	return &Store{client: client}
}

func (s *Store) Set(ctx context.Context, targetType, targetID, userID, kind string, active bool, now time.Time) (bool, int64, error) {
	if s == nil || s.client == nil {
		return false, 0, fmt.Errorf("reaction redis is not configured")
	}
	flag := "0"
	if active {
		flag = "1"
	}
	project := "0"
	if kind == contract.KindLike && projectionType(targetType) {
		project = "1"
	}
	keys := []string{
		setKey(kind, targetID),
		timesKey(targetType),
		userIndexKey(userID, kind, targetType),
	}
	values, err := setScript.Run(ctx, s.client, keys, userID, flag, targetID, kind, project, now.UnixMilli()).Slice()
	if err != nil {
		return false, 0, err
	}
	count := int64(0)
	if len(values) > 0 {
		count, _ = values[0].(int64)
	}
	return active, count, nil
}

func (s *Store) States(ctx context.Context, targetType string, ids []string, userID, kind string) ([]reactionservice.State, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("reaction redis is not configured")
	}
	pipe := s.client.Pipeline()
	counts := make([]*goredis.IntCmd, 0, len(ids))
	liked := make([]*goredis.BoolCmd, 0, len(ids))
	for _, id := range ids {
		counts = append(counts, pipe.SCard(ctx, setKey(kind, id)))
		if strings.TrimSpace(userID) == "" {
			liked = append(liked, nil)
			continue
		}
		liked = append(liked, pipe.SIsMember(ctx, setKey(kind, id), userID))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return nil, err
	}
	out := make([]reactionservice.State, 0, len(ids))
	for i, id := range ids {
		state := reactionservice.State{TargetID: id}
		if i < len(counts) {
			state.Count = counts[i].Val()
		}
		if i < len(liked) && liked[i] != nil {
			state.Active = liked[i].Val()
		}
		out = append(out, state)
	}
	return out, nil
}

func (s *Store) ListMine(ctx context.Context, userID, kind, targetType, cursor string, limit int) ([]reactionservice.Mine, string, error) {
	if s == nil || s.client == nil {
		return nil, "", fmt.Errorf("reaction redis is not configured")
	}
	offset, err := decodeOffset(cursor)
	if err != nil {
		return nil, "", err
	}
	values, err := s.client.ZRevRangeWithScores(ctx, userIndexKey(userID, kind, targetType), int64(offset), int64(offset+limit)).Result()
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(values) > limit {
		values = values[:limit]
		next = encodeOffset(offset + limit)
	}
	out := make([]reactionservice.Mine, 0, len(values))
	for _, value := range values {
		id, ok := value.Member.(string)
		if !ok {
			continue
		}
		out = append(out, reactionservice.Mine{
			TargetID:  id,
			CreatedAt: time.UnixMilli(int64(value.Score)).UTC(),
		})
	}
	return out, next, nil
}

func (s *Store) PopPending(ctx context.Context, targetType string, limit int64) ([]reactionservice.Pending, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("reaction redis is not configured")
	}
	pairs, err := s.client.ZPopMin(ctx, timesKey(targetType), limit).Result()
	if err != nil {
		return nil, err
	}
	out := make([]reactionservice.Pending, 0, len(pairs))
	for _, pair := range pairs {
		id, ok := pair.Member.(string)
		if !ok {
			continue
		}
		out = append(out, reactionservice.Pending{TargetID: id, Count: int64(pair.Score)})
	}
	return out, nil
}

func (s *Store) RequeuePending(ctx context.Context, targetType string, items []reactionservice.Pending) error {
	if s == nil || s.client == nil || len(items) == 0 {
		return nil
	}
	args := make([]any, 0, len(items)*2)
	for _, item := range items {
		args = append(args, item.TargetID, item.Count)
	}
	return requeueScript.Run(ctx, s.client, []string{timesKey(targetType)}, args...).Err()
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("reaction redis is not configured")
	}
	return s.client.Ping(ctx).Err()
}

func setKey(kind, targetID string) string {
	if kind == contract.KindFavorite {
		return favoriteSetPrefix + targetID
	}
	return likeSetPrefix + targetID
}

func timesKey(targetType string) string {
	return timesPrefix + targetType
}

func userIndexKey(userID, kind, targetType string) string {
	return userIndexPrefix + userID + ":" + kind + ":" + targetType
}

func projectionType(targetType string) bool {
	switch targetType {
	case contract.TargetPlaceReview, contract.TargetPlaceComment, contract.TargetRiderReview, contract.TargetRiderComment:
		return true
	default:
		return false
	}
}

func encodeOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeOffset(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid reaction cursor")
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid reaction cursor")
	}
	return offset, nil
}
