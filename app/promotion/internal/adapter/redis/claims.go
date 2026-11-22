package promotionredis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"cityflow/app/promotion/internal/application/promotionapp"
)

const (
	userCouponKeyPrefix = "prs:user:coupon:"
	claimIdemKeyPrefix  = "prs:claim:idem:"
	defaultStreamMaxLen = 100000
)

var _ promotionapp.ReservationStore = (*ClaimStream)(nil)
var _ promotionapp.ClaimQueue = (*ClaimStream)(nil)

// ClaimStream 把预扣、幂等结果和消费者队列收敛到同一个 Redis 实例：
// 预扣 Lua 里同时完成 HSET 幂等键与 XADD，进程在中间崩溃也不会出现
// “库存扣了但消息没发”的缺口。
type ClaimStream struct {
	client    *goredis.Client
	stream    string
	group     string
	resultTTL time.Duration
	maxLen    int64
}

func NewClaimStream(client *goredis.Client, stream, group string, resultTTL time.Duration) *ClaimStream {
	if stream == "" {
		stream = "prs:claim:stream"
	}
	if group == "" {
		group = "promotion-claim"
	}
	if resultTTL <= 0 {
		resultTTL = 24 * time.Hour
	}
	return &ClaimStream{client: client, stream: stream, group: group, resultTTL: resultTTL, maxLen: defaultStreamMaxLen}
}

// WithMaxLen 设置 Stream 的硬上限，用于测试或按实例容量调整。
// 超过上限时 Reserve 返回 busy，而不是继续扣库存堆消息。
func (s *ClaimStream) WithMaxLen(maxLen int64) *ClaimStream {
	if maxLen > 0 {
		s.maxLen = maxLen
	}
	return s
}

func (s *ClaimStream) CouponKey(couponID string) string {
	return couponKeyPrefix + couponID
}

// Stream / Group / Key 暴露给运维与测试，不参与业务逻辑。
func (s *ClaimStream) Stream() string { return s.stream }
func (s *ClaimStream) Group() string  { return s.group }
func (s *ClaimStream) Key(couponID string) string {
	return s.CouponKey(couponID)
}

// IdemKey 暴露幂等键的实际 Redis key，仅供测试清理和运维排查。
func (s *ClaimStream) IdemKey(userID, couponID, idempotencyKey string) string {
	return s.idemKey(userID, couponID, idempotencyKey)
}

func (s *ClaimStream) userKey(couponID string) string {
	return userCouponKeyPrefix + couponID
}

func (s *ClaimStream) idemKey(userID, couponID, idempotencyKey string) string {
	// 幂等键的语义域是 (user, coupon, key)，不是全局 key：不同用户可能碰巧
	// 使用同一个客户端生成的键，不能互相顶掉。这里做定长哈希，避免拼接歧义。
	sum := sha256.Sum256([]byte(userID + "\x00" + couponID + "\x00" + idempotencyKey))
	return claimIdemKeyPrefix + hex.EncodeToString(sum[:16])
}

var reserveScript = goredis.NewScript(`
-- KEYS: 1 coupon hash, 2 user hash, 3 idempotency hash, 4 stream
-- ARGV: 1 couponID, 2 userID, 3 idemKey, 4 claimID, 5 nowMs, 6 resultTTLms, 7 streamMaxLen
local existing = redis.call('HGETALL', KEYS[3])
if #existing > 0 then
    local data = {}
    for i = 1, #existing, 2 do
        data[existing[i]] = existing[i + 1]
    end
    if data['coupon'] ~= ARGV[1] or data['user'] ~= ARGV[2] then
        return {'conflict'}
    end
    return {data['status'] or 'pending', data['claimId'] or '', data['reason'] or ''}
end
if redis.call('EXISTS', KEYS[1]) == 0 then
    return {'miss'}
end
if redis.call('XLEN', KEYS[4]) >= tonumber(ARGV[7]) then
    return {'reject', 'busy'}
end
if redis.call('HGET', KEYS[1], 'status') ~= 'ACTIVE' then
    return {'reject', 'invalid'}
end
local starts = redis.call('HGET', KEYS[1], 'startsAt')
if starts and starts ~= '' and tonumber(ARGV[5]) < tonumber(starts) then
    return {'reject', 'invalid'}
end
local ends = redis.call('HGET', KEYS[1], 'endsAt')
if ends and ends ~= '' and tonumber(ARGV[5]) >= tonumber(ends) then
    return {'reject', 'invalid'}
end
local remaining = tonumber(redis.call('HGET', KEYS[1], 'remaining') or '0')
if remaining <= 0 then
    return {'reject', 'soldout'}
end
local limit = tonumber(redis.call('HGET', KEYS[1], 'userLimit') or '0')
if limit > 0 then
    local used = tonumber(redis.call('HGET', KEYS[2], ARGV[2]) or '0')
    if used >= limit then
        return {'reject', 'limit'}
    end
end
redis.call('HINCRBY', KEYS[1], 'remaining', -1)
if limit > 0 then
    redis.call('HINCRBY', KEYS[2], ARGV[2], 1)
    redis.call('PEXPIRE', KEYS[2], ARGV[6])
end
local eventID = redis.call('XADD', KEYS[4], '*',
    'claim', ARGV[4], 'coupon', ARGV[1], 'user', ARGV[2], 'idem', ARGV[3])
redis.call('HSET', KEYS[3],
    'status', 'pending', 'claimId', ARGV[4], 'coupon', ARGV[1], 'user', ARGV[2], 'event', eventID)
redis.call('PEXPIRE', KEYS[3], ARGV[6])
return {'ok', eventID, ARGV[4]}
`)

// Reserve 是原子预扣：校验规则、扣剩余、累计限领、写幂等键、XADD 一次完成。
func (s *ClaimStream) Reserve(ctx context.Context, userID, couponID, idempotencyKey, claimID string, now time.Time) (*promotionapp.ClaimReservation, error) {
	values, err := reserveScript.Run(ctx, s.client,
		[]string{s.CouponKey(couponID), s.userKey(couponID), s.idemKey(userID, couponID, idempotencyKey), s.stream},
		couponID, userID, idempotencyKey, claimID,
		strconv.FormatInt(now.UnixMilli(), 10), strconv.FormatInt(s.resultTTL.Milliseconds(), 10),
		strconv.FormatInt(s.maxLen, 10),
	).Slice()
	if err != nil {
		return nil, fmt.Errorf("reserve coupon %s: %w", couponID, err)
	}
	reservation := &promotionapp.ClaimReservation{
		CouponID: couponID, UserID: userID, IdempotencyKey: idempotencyKey, ClaimID: claimID,
	}
	if len(values) == 0 {
		reservation.Status = "miss"
		return reservation, nil
	}
	reservation.Status = valueString(values[0])
	if len(values) > 1 {
		switch reservation.Status {
		case "reject":
			reservation.Reason = valueString(values[1])
		case "ok":
			reservation.EventID = valueString(values[1])
			if len(values) > 2 {
				reservation.ClaimID = valueString(values[2])
			}
		}
	}
	return reservation, nil
}

var releaseScript = goredis.NewScript(`
-- KEYS: 1 coupon hash, 2 user hash, 3 idempotency hash
-- ARGV: 1 userID, 2 reason, 3 resultTTLms
local status = redis.call('HGET', KEYS[3], 'status')
if not status or status ~= 'pending' then
    return 0
end
if redis.call('EXISTS', KEYS[1]) == 1 then
    redis.call('HINCRBY', KEYS[1], 'remaining', 1)
    local limit = tonumber(redis.call('HGET', KEYS[1], 'userLimit') or '0')
    if limit > 0 then
        local used = tonumber(redis.call('HGET', KEYS[2], ARGV[1]) or '0')
        if used > 0 then
            redis.call('HINCRBY', KEYS[2], ARGV[1], -1)
        end
    end
end
redis.call('HSET', KEYS[3], 'status', 'failed', 'reason', ARGV[2])
redis.call('PEXPIRE', KEYS[3], ARGV[3])
return 1
`)

// Release 补偿一次失败的预约：库存加回、限领计数减回、结果标记 failed。
// status 已经是 ok/failed 时直接返回，保证重复消费不会二次补偿。
func (s *ClaimStream) Release(ctx context.Context, reservation *promotionapp.ClaimReservation, reason string) error {
	if reservation == nil {
		return nil
	}
	_, err := releaseScript.Run(ctx, s.client,
		[]string{s.CouponKey(reservation.CouponID), s.userKey(reservation.CouponID), s.idemKey(reservation.UserID, reservation.CouponID, reservation.IdempotencyKey)},
		reservation.UserID, reason, strconv.FormatInt(s.resultTTL.Milliseconds(), 10),
	).Int()
	if err != nil {
		return fmt.Errorf("release coupon reservation %s: %w", reservation.EventID, err)
	}
	return nil
}

// Complete 把消费结果写回幂等键，供请求方轮询。
func (s *ClaimStream) Complete(ctx context.Context, reservation *promotionapp.ClaimReservation, claimID, status, reason string) error {
	if reservation == nil {
		return nil
	}
	values := map[string]any{"status": status}
	if claimID != "" {
		values["claimId"] = claimID
	}
	if reason != "" {
		values["reason"] = reason
	}
	pipe := s.client.TxPipeline()
	key := s.idemKey(reservation.UserID, reservation.CouponID, reservation.IdempotencyKey)
	pipe.HSet(ctx, key, values)
	pipe.PExpire(ctx, key, s.resultTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("complete coupon reservation %s: %w", reservation.EventID, err)
	}
	return nil
}

// Result 返回某个 (user, coupon, idempotencyKey) 上的当前结果；不存在时 found=false。
func (s *ClaimStream) Result(ctx context.Context, userID, couponID, idempotencyKey string) (*promotionapp.ClaimReservation, bool, error) {
	data, err := s.client.HGetAll(ctx, s.idemKey(userID, couponID, idempotencyKey)).Result()
	if err != nil {
		return nil, false, fmt.Errorf("read coupon reservation %s: %w", idempotencyKey, err)
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	reservation := &promotionapp.ClaimReservation{
		EventID: data["event"], ClaimID: data["claimId"], CouponID: data["coupon"],
		UserID: data["user"], IdempotencyKey: idempotencyKey, Status: data["status"], Reason: data["reason"],
	}
	if reservation.Status == "" {
		reservation.Status = "pending"
	}
	return reservation, true, nil
}

// EnsureGroup 创建消费者组。已存在（BUSYGROUP）视为成功。
// 起点用 0 而不是 $：服务启动瞬间可能已经有 Reserve 写入的消息，
// 如果组从“当前末尾”开始，这些已扣库存的事件会永远没人消费。
func (s *ClaimStream) EnsureGroup(ctx context.Context) error {
	err := s.client.XGroupCreateMkStream(ctx, s.stream, s.group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create claim group %s: %w", s.group, err)
	}
	return nil
}

// Read 读一批新消息。没有消息时返回空切片，不把 redis.Nil 当错误。
func (s *ClaimStream) Read(ctx context.Context, consumer string, count int64, block time.Duration) ([]*promotionapp.ClaimReservation, error) {
	streams, err := s.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group: s.group, Consumer: consumer,
		Streams: []string{s.stream, ">"}, Count: count, Block: block,
	}).Result()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read claim stream: %w", err)
	}
	var result []*promotionapp.ClaimReservation
	for _, stream := range streams {
		for _, message := range stream.Messages {
			result = append(result, messageReservation(message))
		}
	}
	return result, nil
}

func (s *ClaimStream) Ack(ctx context.Context, eventID string) error {
	return s.client.XAck(ctx, s.stream, s.group, eventID).Err()
}

// Reclaim 把空闲超过 minIdle 的 pending 消息重新分配给当前消费者。
func (s *ClaimStream) Reclaim(ctx context.Context, consumer string, minIdle time.Duration, count int64) ([]*promotionapp.ClaimReservation, error) {
	messages, _, err := s.client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
		Stream: s.stream, Group: s.group, Consumer: consumer,
		MinIdle: minIdle, Start: "0-0", Count: count,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("reclaim claim stream: %w", err)
	}
	result := make([]*promotionapp.ClaimReservation, 0, len(messages))
	for _, message := range messages {
		result = append(result, messageReservation(message))
	}
	return result, nil
}

// Trim 只在没有 pending（未 ACK）消息时裁剪历史条目。
// 有 pending 时不动，避免把还没落库的预约裁掉；未投递的新消息在末尾，不受影响。
func (s *ClaimStream) Trim(ctx context.Context) error {
	pending, err := s.client.XPending(ctx, s.stream, s.group).Result()
	if err != nil {
		if strings.Contains(err.Error(), "NOGROUP") {
			return nil
		}
		return fmt.Errorf("inspect claim stream pending: %w", err)
	}
	if pending != nil && pending.Count > 0 {
		return nil
	}
	if err := s.client.XTrimMaxLen(ctx, s.stream, s.maxLen).Err(); err != nil {
		return fmt.Errorf("trim claim stream: %w", err)
	}
	return nil
}

func messageReservation(message goredis.XMessage) *promotionapp.ClaimReservation {
	get := func(key string) string {
		if value, ok := message.Values[key]; ok {
			return fmt.Sprint(value)
		}
		return ""
	}
	return &promotionapp.ClaimReservation{
		EventID: message.ID, ClaimID: get("claim"), CouponID: get("coupon"),
		UserID: get("user"), IdempotencyKey: get("idem"), Status: "pending",
	}
}

func valueString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
