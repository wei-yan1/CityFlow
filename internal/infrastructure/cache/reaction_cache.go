package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"cityflow/internal/domain/reaction"
)

const reactionStatePrefix = "cityflow:reaction:state:"
const reactionCountPrefix = "cityflow:reaction:like:count:"
const reactionProjectionPrefix = "cityflow:reaction:like:projection:"
const reactionOpsStream = "cityflow:reaction:ops"

// The script keeps the user set, the display counter, the projection queue and
// the durable-op stream in one atomic step. A pipeline could leave the state
// changed if the process died between SADD and XADD; this cannot.
var likeScript = redis.NewScript(`
local changed
if ARGV[2] == '1' then
  changed = redis.call('SADD', KEYS[1], ARGV[1])
else
  changed = redis.call('SREM', KEYS[1], ARGV[1])
end
local count = redis.call('SCARD', KEYS[1])
if changed == 1 then
  if ARGV[5] == 'LIKE' and ARGV[6] == '1' then
    redis.call('ZADD', KEYS[2], count, ARGV[4])
    redis.call('ZADD', KEYS[3], count, ARGV[4])
  elseif ARGV[5] == 'LIKE' then
    -- Display counters are needed for comments and rider reviews too, but
    -- only owners with a like_count column get a projection queue entry.
    redis.call('ZADD', KEYS[2], count, ARGV[4])
  end
  redis.call('XADD', KEYS[4], 'MAXLEN', '~', '2000000', '*',
    'type', ARGV[3], 'id', ARGV[4], 'user', ARGV[1], 'kind', ARGV[5], 'op', ARGV[2])
end
return {changed, count}
`)

// ProjectionTypes limits the projection ZSet to targets whose owner actually
// has a like_count column and a consumer. Without this gate, comment likes
// would accumulate in a queue nobody drains.
type ReactionCache struct {
	Client          *redis.Client
	ProjectionTypes map[string]struct{}
}

func NewReactionCache(client *redis.Client) *ReactionCache {
	return &ReactionCache{
		Client:          client,
		ProjectionTypes: map[string]struct{}{reaction.TargetPlaceReview: {}},
	}
}

func (c *ReactionCache) SetProjectionTypes(types []string) {
	if c == nil || len(types) == 0 {
		return
	}
	next := make(map[string]struct{}, len(types))
	for _, targetType := range types {
		if targetType = strings.TrimSpace(targetType); targetType != "" && reaction.ValidTargetType(targetType) {
			next[targetType] = struct{}{}
		}
	}
	if len(next) > 0 {
		c.ProjectionTypes = next
	}
}

func (c *ReactionCache) Set(ctx context.Context, targetType, targetID, userID, kind string, active bool) (bool, int64, error) {
	if c == nil || c.Client == nil {
		return false, 0, fmt.Errorf("reaction redis is not configured")
	}
	flag := "0"
	if active {
		flag = "1"
	}
	project := "0"
	if c.ProjectionTypes != nil {
		if _, ok := c.ProjectionTypes[targetType]; ok {
			project = "1"
		}
	}
	values, err := likeScript.Run(ctx, c.Client, []string{
		reactionStateKey(targetType, targetID, kind),
		reactionCountKey(targetType),
		reactionProjectionKey(targetType),
		reactionOpsStream,
	}, userID, flag, targetType, targetID, kind, project).Slice()
	if err != nil {
		return false, 0, err
	}
	changed := int64(0)
	count := int64(0)
	if len(values) > 0 {
		changed, _ = values[0].(int64)
	}
	if len(values) > 1 {
		count, _ = values[1].(int64)
	}
	return changed == 1, count, nil
}

type State struct {
	TargetID   string
	Count      int64
	CountKnown bool
	Liked      bool
}

func (c *ReactionCache) GetStates(ctx context.Context, targetType string, targetIDs []string, userID, kind string) ([]State, error) {
	if c == nil || c.Client == nil {
		return nil, fmt.Errorf("reaction redis is not configured")
	}
	pipe := c.Client.Pipeline()
	counts := make([]*redis.FloatCmd, 0, len(targetIDs))
	liked := make([]*redis.BoolCmd, 0, len(targetIDs))
	for _, id := range targetIDs {
		if kind == reaction.KindLike {
			counts = append(counts, pipe.ZScore(ctx, reactionCountKey(targetType), id))
		}
		liked = append(liked, pipe.SIsMember(ctx, reactionStateKey(targetType, id, kind), userID))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]State, 0, len(targetIDs))
	for i, id := range targetIDs {
		state := State{TargetID: id}
		if len(counts) > i {
			if value, err := counts[i].Result(); err == nil {
				state.Count = int64(value)
				state.CountKnown = true
			}
		}
		if len(liked) > i {
			state.Liked = liked[i].Val()
		}
		out = append(out, state)
	}
	return out, nil
}

// PopProjection atomically takes the next count batch. The returned score is
// the absolute count at the time the item was last written.
type Projection struct {
	TargetID string
	Count    int64
}

func (c *ReactionCache) PopProjection(ctx context.Context, targetType string, limit int64) ([]Projection, error) {
	if c == nil || c.Client == nil {
		return nil, fmt.Errorf("reaction redis is not configured")
	}
	pairs, err := c.Client.ZPopMin(ctx, reactionProjectionKey(targetType), limit).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Projection, 0, len(pairs))
	for _, pair := range pairs {
		id, ok := pair.Member.(string)
		if !ok {
			continue
		}
		out = append(out, Projection{TargetID: id, Count: int64(pair.Score)})
	}
	return out, nil
}

func (c *ReactionCache) RequeueProjection(ctx context.Context, targetType string, items []Projection) error {
	if c == nil || c.Client == nil || len(items) == 0 {
		return nil
	}
	args := make([]redis.Z, 0, len(items))
	for _, item := range items {
		args = append(args, redis.Z{Score: float64(item.Count), Member: item.TargetID})
	}
	return c.Client.ZAdd(ctx, reactionProjectionKey(targetType), args...).Err()
}

type StreamOp struct {
	MessageID  string
	TargetType string
	TargetID   string
	UserID     string
	Kind       string
	Active     bool
}

func (c *ReactionCache) ReadOps(ctx context.Context, consumer string, count int64, block time.Duration) ([]StreamOp, error) {
	if c == nil || c.Client == nil {
		return nil, fmt.Errorf("reaction redis is not configured")
	}
	err := c.Client.XGroupCreateMkStream(ctx, reactionOpsStream, "cg-reaction", "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, err
	}
	streams, err := c.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: "cg-reaction", Consumer: consumer,
		Streams: []string{reactionOpsStream, ">"}, Count: count, Block: block,
	}).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]StreamOp, 0)
	invalid := make([]string, 0)
	for _, stream := range streams {
		for _, msg := range stream.Messages {
			op := StreamOp{MessageID: msg.ID}
			op.TargetType = stringField(msg.Values, "type")
			op.TargetID = stringField(msg.Values, "id")
			op.UserID = stringField(msg.Values, "user")
			op.Kind = stringField(msg.Values, "kind")
			op.Active = stringField(msg.Values, "op") == "1"
			if op.TargetType == "" || op.TargetID == "" || op.UserID == "" || !reaction.ValidTargetType(op.TargetType) || !reaction.ValidKind(op.Kind) {
				invalid = append(invalid, msg.ID)
				continue
			}
			out = append(out, op)
		}
	}
	// Invalid messages can never be applied; ack them here so a malformed
	// producer cannot grow the pending list forever.
	if len(invalid) > 0 {
		_ = c.AckOps(ctx, invalid...)
	}
	return out, nil
}

func (c *ReactionCache) AckOps(ctx context.Context, ids ...string) error {
	if c == nil || c.Client == nil || len(ids) == 0 {
		return nil
	}
	return c.Client.XAck(ctx, reactionOpsStream, "cg-reaction", ids...).Err()
}

func (c *ReactionCache) Rebuild(ctx context.Context, targetType string, records []reaction.Record) error {
	if c == nil || c.Client == nil {
		return fmt.Errorf("reaction redis is not configured")
	}
	pipe := c.Client.Pipeline()
	pipe.Del(ctx, reactionCountKey(targetType), reactionProjectionKey(targetType))
	counts := map[string]int64{}
	for _, record := range records {
		if record.Kind == reaction.KindLike {
			counts[record.TargetID]++
			pipe.SAdd(ctx, reactionStateKey(record.TargetType, record.TargetID, record.Kind), record.UserID)
		} else {
			pipe.SAdd(ctx, reactionStateKey(record.TargetType, record.TargetID, record.Kind), record.UserID)
		}
	}
	for id, count := range counts {
		pipe.ZAdd(ctx, reactionCountKey(targetType), redis.Z{Score: float64(count), Member: id})
	}
	_, err := pipe.Exec(ctx)
	return err
}

func reactionStateKey(targetType, targetID, kind string) string {
	return reactionStatePrefix + kind + ":" + targetType + ":" + targetID
}

func reactionCountKey(targetType string) string { return reactionCountPrefix + targetType }
func reactionProjectionKey(targetType string) string {
	return reactionProjectionPrefix + targetType
}

func stringField(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func ProjectionMessage(targetType string, items []Projection) ([]byte, error) {
	type item struct {
		TargetID string `json:"target_id"`
		Count    int64  `json:"count"`
	}
	out := make([]item, 0, len(items))
	for _, value := range items {
		out = append(out, item{TargetID: value.TargetID, Count: value.Count})
	}
	return json.Marshal(map[string]any{"type": targetType, "items": out})
}
