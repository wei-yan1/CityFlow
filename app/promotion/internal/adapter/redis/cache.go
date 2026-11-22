// Package promotionredis 持有领券链路在 Redis 里的两套状态：
//
//	prs:coupon:<couponID>   Hash：状态、起止时间、总量、剩余、每人限领（只读闸门 + 预扣共用）
//
// 本文件只实现“缓存 + 只读校验”，不改变库存事实：库存权威仍在 MySQL。
// 条件更新成功后才做一次 best-effort 的 remaining-1，失败由刷新任务按 DB 重算。
package promotionredis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"cityflow/app/promotion/internal/application/promotionapp"
	"cityflow/app/promotion/internal/domain/coupon"
)

const (
	couponKeyPrefix = "prs:coupon:"
	defaultCacheTTL = 10 * time.Minute
)

var _ promotionapp.CouponGate = (*Cache)(nil)

type Cache struct {
	client *goredis.Client
	ttl    time.Duration
}

func New(client *goredis.Client) *Cache {
	return &Cache{client: client, ttl: defaultCacheTTL}
}

func (c *Cache) Key(couponID string) string { return couponKeyPrefix + couponID }

// Warm 把一张券的规则快照写入 Hash。remaining 取 DB 投影，不信任调用方传入的旧值。
func (c *Cache) Warm(ctx context.Context, item *coupon.Coupon) error {
	if item == nil || item.ID == "" {
		return nil
	}
	remaining := item.TotalQuantity - item.ClaimedCount
	if remaining < 0 {
		remaining = 0
	}
	values := map[string]any{
		"status":    item.Status,
		"startsAt":  epochMillis(item.StartsAt),
		"endsAt":    epochMillis(item.EndsAt),
		"total":     item.TotalQuantity,
		"remaining": remaining,
		"userLimit": item.PerUserLimit,
	}
	pipe := c.client.TxPipeline()
	pipe.HSet(ctx, c.Key(item.ID), values)
	pipe.PExpire(ctx, c.Key(item.ID), c.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("warm coupon cache %s: %w", item.ID, err)
	}
	return nil
}

func (c *Cache) Delete(ctx context.Context, couponID string) error {
	return c.client.Del(ctx, c.Key(couponID)).Err()
}

var warmRulesScript = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
    redis.call('HSET', KEYS[1], 'remaining', ARGV[5])
end
redis.call('HSET', KEYS[1],
    'status', ARGV[1], 'startsAt', ARGV[2], 'endsAt', ARGV[3],
    'total', ARGV[4], 'userLimit', ARGV[6])
redis.call('PEXPIRE', KEYS[1], ARGV[7])
return 1
`)

// WarmRules 只刷新规则字段，不覆盖已有的 remaining。
// 用途：定时刷新不能把异步链路尚未落库的在途预扣“加回库存”。
func (c *Cache) WarmRules(ctx context.Context, item *coupon.Coupon) error {
	if item == nil || item.ID == "" {
		return nil
	}
	remaining := item.TotalQuantity - item.ClaimedCount
	if remaining < 0 {
		remaining = 0
	}
	_, err := warmRulesScript.Run(ctx, c.client, []string{c.Key(item.ID)},
		item.Status, epochMillis(item.StartsAt), epochMillis(item.EndsAt),
		strconv.Itoa(item.TotalQuantity), strconv.Itoa(remaining), strconv.Itoa(item.PerUserLimit),
		strconv.FormatInt(c.ttl.Milliseconds(), 10),
	).Int()
	if err != nil {
		return fmt.Errorf("refresh coupon rules %s: %w", item.ID, err)
	}
	return nil
}

var gateScript = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
    return 0
end
if redis.call('HGET', KEYS[1], 'status') ~= 'ACTIVE' then
    return 1
end
local starts = redis.call('HGET', KEYS[1], 'startsAt')
if starts and starts ~= '' and tonumber(ARGV[1]) < tonumber(starts) then
    return 2
end
local ends = redis.call('HGET', KEYS[1], 'endsAt')
if ends and ends ~= '' and tonumber(ARGV[1]) >= tonumber(ends) then
    return 3
end
local remaining = redis.call('HGET', KEYS[1], 'remaining')
if remaining and tonumber(remaining) <= 0 then
    return 4
end
return 100
`)

// Check 是只读校验：不扣减、不写任何字段。返回 GateMiss 时调用方回源 DB 并尝试 Warm。
func (c *Cache) Check(ctx context.Context, couponID string, now time.Time) (promotionapp.GateDecision, string, error) {
	res, err := gateScript.Run(ctx, c.client, []string{c.Key(couponID)}, strconv.FormatInt(now.UnixMilli(), 10)).Int()
	if err != nil {
		return promotionapp.GateMiss, "", fmt.Errorf("check coupon cache %s: %w", couponID, err)
	}
	switch res {
	case 0:
		return promotionapp.GateMiss, "", nil
	case 100:
		return promotionapp.GatePass, "", nil
	case 1:
		return promotionapp.GateReject, "该券当前不可领取", nil
	case 2:
		return promotionapp.GateReject, "该券尚未开始", nil
	case 3:
		return promotionapp.GateReject, "该券已结束", nil
	case 4:
		return promotionapp.GateReject, "该券已领完", nil
	default:
		return promotionapp.GateMiss, "", nil
	}
}

var consumeScript = goredis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
    return 0
end
local remaining = tonumber(redis.call('HGET', KEYS[1], 'remaining') or '0')
if remaining <= 0 then
    return 0
end
redis.call('HINCRBY', KEYS[1], 'remaining', -1)
return 1
`)

// Consume 只在 DB 条件更新成功之后调用，用来把缓存剩余量往前推一格。
// 它不是库存事实：失败、漏做、Redis 重启都由刷新任务按 DB 重算。
func (c *Cache) Consume(ctx context.Context, couponID string) error {
	if _, err := consumeScript.Run(ctx, c.client, []string{c.Key(couponID)}).Int(); err != nil {
		return fmt.Errorf("consume coupon cache %s: %w", couponID, err)
	}
	return nil
}

// Remaining 返回缓存里的剩余量，仅用于对账任务。
func (c *Cache) Remaining(ctx context.Context, couponID string) (int64, bool, error) {
	value, err := c.client.HGet(ctx, c.Key(couponID), "remaining").Int64()
	if err == goredis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

// Reset 用 DB 的权威值覆盖缓存剩余量，供对账任务使用。
func (c *Cache) Reset(ctx context.Context, item *coupon.Coupon) error {
	return c.Warm(ctx, item)
}

func epochMillis(value *time.Time) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(value.UnixMilli(), 10)
}
