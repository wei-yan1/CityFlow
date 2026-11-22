package promotionapp

import (
	"context"
	"fmt"
	"log"
	"time"
)

const (
	claimMaintenanceBatch = 1000
	claimMaintenancePass  = 5
	staleClaimLockAfter   = 7 * 24 * time.Hour

	couponCachePageSize = 200
	couponCacheMaxItems = 5000
)

// RefreshCouponCache 把 ACTIVE 且处于有效期内的券刷新到 Redis。
// 任务幂等；过期券不主动删，靠缓存 TTL 自然淘汰。
func (s *Service) RefreshCouponCache(ctx context.Context) (int, error) {
	if s.gate == nil {
		return 0, nil
	}
	total := 0
	for offset := 0; offset < couponCacheMaxItems; offset += couponCachePageSize {
		items, err := s.store.ListCoupons(ctx, "", s.now(), Page{Limit: couponCachePageSize, Offset: offset})
		if err != nil {
			return total, fmt.Errorf("list coupons for cache refresh: %w", err)
		}
		for _, item := range items {
			warm := s.gate.WarmRules
			if s.async == nil {
				// 同步模式没有在途预扣，缓存与 DB 的任何偏差都是漂移，直接按 DB 重算。
				warm = s.gate.Warm
			}
			if err := warm(ctx, item); err != nil {
				return total, err
			}
			total++
		}
		if len(items) < couponCachePageSize {
			break
		}
	}
	return total, nil
}

// ExpireClaims persists the two claim states that were previously only
// projected at read time: coupons whose campaign has ended, and LOCKED claims
// abandoned by a checkout that never reached payment.
//
// Release runs first so an expired campaign's stale LOCKED claim becomes
// UNUSED and is then picked up by the expire pass in the same invocation.
func (s *Service) ExpireClaims(ctx context.Context) (released, expired int64, err error) {
	now := s.now()
	released, err = s.runClaimBatches(ctx, func() (int64, error) {
		return s.store.ReleaseStaleLocked(ctx, now.Add(-staleClaimLockAfter), claimMaintenanceBatch)
	})
	if err != nil {
		return released, expired, fmt.Errorf("release stale coupon claims: %w", err)
	}
	expired, err = s.runClaimBatches(ctx, func() (int64, error) {
		return s.store.ExpireClaims(ctx, now, claimMaintenanceBatch)
	})
	if err != nil {
		return released, expired, fmt.Errorf("expire coupon claims: %w", err)
	}
	return released, expired, nil
}

// ReconcileCouponCache 对账只做“向下修正”：缓存剩余量大于 DB 时立即按 DB 重置，
// 防止闸门放过过多请求；缓存小于 DB 视为在途预扣（异步链路未落库），只记日志，
// 不用 DB 值硬覆盖，否则会把还没落库的预约库存又加回去。
func (s *Service) ReconcileCouponCache(ctx context.Context) (checked, repaired int, err error) {
	if s.gate == nil {
		return 0, 0, nil
	}
	for offset := 0; offset < couponCacheMaxItems; offset += couponCachePageSize {
		items, listErr := s.store.ListCoupons(ctx, "", s.now(), Page{Limit: couponCachePageSize, Offset: offset})
		if listErr != nil {
			return checked, repaired, fmt.Errorf("list coupons for reconcile: %w", listErr)
		}
		for _, item := range items {
			checked++
			cached, ok, cacheErr := s.gate.Remaining(ctx, item.ID)
			if cacheErr != nil {
				return checked, repaired, cacheErr
			}
			if !ok {
				continue
			}
			dbRemaining := int64(item.Remaining)
			if cached == dbRemaining {
				continue
			}
			if cached > dbRemaining || s.async == nil {
				if resetErr := s.gate.Reset(ctx, item); resetErr != nil {
					return checked, repaired, resetErr
				}
				repaired++
				log.Printf("promotion: coupon %s cache remaining %d != db %d, reset to db", item.ID, cached, dbRemaining)
			} else {
				log.Printf("promotion: coupon %s cache remaining %d < db %d, in-flight reservations", item.ID, cached, dbRemaining)
			}
		}
		if len(items) < couponCachePageSize {
			break
		}
	}
	return checked, repaired, nil
}

func (s *Service) runClaimBatches(ctx context.Context, fn func() (int64, error)) (int64, error) {
	var total int64
	for pass := 0; pass < claimMaintenancePass; pass++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := fn()
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < claimMaintenanceBatch {
			break
		}
	}
	return total, nil
}
