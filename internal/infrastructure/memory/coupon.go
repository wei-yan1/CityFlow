package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/coupon"
)

var _ repository.CouponRepository = (*Store)(nil)

func cloneCoupon(item *coupon.Coupon) *coupon.Coupon {
	if item == nil {
		return nil
	}
	copy := *item
	copy.StartsAt = copyTime(item.StartsAt)
	copy.EndsAt = copyTime(item.EndsAt)
	copy.ApplicableTaskTypes = append([]string(nil), item.ApplicableTaskTypes...)
	return &copy
}

func cloneUserCoupon(item *coupon.UserCoupon) *coupon.UserCoupon {
	if item == nil {
		return nil
	}
	copy := *item
	copy.UsedAt = copyTime(item.UsedAt)
	copy.Coupon = cloneCoupon(item.Coupon)
	return &copy
}

func claimPriority(status string) int {
	switch status {
	case coupon.ClaimUnused:
		return 3
	case coupon.ClaimLocked:
		return 2
	case coupon.ClaimUsed:
		return 1
	default:
		return 0
	}
}

func (s *Store) seedCoupons(now time.Time) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 12, 31, 23, 59, 59, 0, time.UTC)
	seeds := []*coupon.Coupon{
		{
			ID: "00000000-0000-4000-8000-000000000101", Code: "WELCOME5",
			Title: "新客立减 5 元", Subtitle: "第一单配送，平台替你省一点",
			Description: "限 CityFlow 用户领取，单笔订单满 10 元可用。",
			Type:        coupon.TypeAmount, ValueFen: 500, MinSpendFen: 1000,
			TotalQuantity: 500, PerUserLimit: 1, Status: coupon.StatusActive,
			StartsAt: &start, EndsAt: &end, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "00000000-0000-4000-8000-000000000102", Code: "RIDE20",
			Title: "配送费 8 折", Subtitle: "适合校园、社区内的短途配送",
			Description: "按订单金额的 20% 抵扣，最高优惠 8 元，单笔满 20 元可用。",
			Type:        coupon.TypePercent, DiscountPercent: 20, MaxDiscountFen: 800, MinSpendFen: 2000,
			TotalQuantity: 300, PerUserLimit: 1, Status: coupon.StatusActive,
			StartsAt: &start, EndsAt: &end, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "00000000-0000-4000-8000-000000000103", Code: "CAMPUS3",
			Title: "校园配送立减 3 元", Subtitle: "取件或送达在校园内时使用",
			Description: "限配送类任务，单笔订单满 8 元可用。",
			Type:        coupon.TypeAmount, ValueFen: 300, MinSpendFen: 800,
			ApplicableTaskTypes: []string{"DELIVERY"},
			TotalQuantity:       1000, PerUserLimit: 1, Status: coupon.StatusActive,
			StartsAt: &start, EndsAt: &end, CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, item := range seeds {
		s.coupons[item.ID] = cloneCoupon(item)
	}
}

func (s *Store) ListCoupons(_ context.Context, userID string, now time.Time, page repository.Page) ([]*coupon.Coupon, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*coupon.Coupon, 0, len(s.coupons))
	for _, item := range s.coupons {
		if !item.ActiveAt(now) {
			continue
		}
		copy := cloneCoupon(item)
		copy.Remaining = copy.TotalQuantity - copy.ClaimedCount
		if copy.Remaining < 0 {
			copy.Remaining = 0
		}
		if userID != "" {
			for _, claim := range s.couponClaims {
				if claim.CouponID != item.ID || claim.UserID != userID {
					continue
				}
				copy.UserClaimCount++
				if copy.UserCouponID == "" || claimPriority(claim.Status) > claimPriority(copy.UserCouponStatus) {
					copy.UserCouponID, copy.UserCouponStatus = claim.ID, claim.Status
				}
			}
		}
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return pageCoupons(items, page), nil
}

func (s *Store) ListUserCoupons(_ context.Context, userID, status string, page repository.Page) ([]*coupon.UserCoupon, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*coupon.UserCoupon, 0)
	for _, claim := range s.couponClaims {
		if claim.UserID != userID || (status != "" && claim.Status != status) {
			continue
		}
		copy := cloneUserCoupon(claim)
		if copy.Coupon == nil {
			copy.Coupon = cloneCoupon(s.coupons[claim.CouponID])
		}
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ClaimedAt.Equal(items[j].ClaimedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].ClaimedAt.After(items[j].ClaimedAt)
	})
	return pageUserCoupons(items, page), nil
}

func (s *Store) UserCoupon(_ context.Context, userID, id string) (*coupon.UserCoupon, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	claim := s.couponClaims[id]
	if claim == nil || claim.UserID != userID {
		return nil, domain.ErrNotFound
	}
	item := cloneUserCoupon(claim)
	if item.Coupon == nil {
		item.Coupon = cloneCoupon(s.coupons[claim.CouponID])
	}
	return item, nil
}

func (s *Store) ClaimCoupon(_ context.Context, userID, couponID string, now time.Time) (*coupon.UserCoupon, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign := s.coupons[couponID]
	if campaign == nil {
		return nil, domain.ErrNotFound
	}
	if !campaign.ActiveAt(now) {
		return nil, fmt.Errorf("%w: 该券当前不可领取", domain.ErrConflict)
	}
	if campaign.ClaimedCount >= campaign.TotalQuantity {
		return nil, fmt.Errorf("%w: 该券已领完", domain.ErrConflict)
	}
	count := 0
	for _, claim := range s.couponClaims {
		if claim.CouponID == couponID && claim.UserID == userID {
			count++
		}
	}
	if count >= campaign.PerUserLimit {
		return nil, fmt.Errorf("%w: 你已达到该券的领取上限", domain.ErrConflict)
	}
	id, err := coupon.NewID()
	if err != nil {
		return nil, err
	}
	claim := &coupon.UserCoupon{
		ID: id, CouponID: couponID, UserID: userID, ClaimNo: count + 1,
		Status: coupon.ClaimUnused, ClaimedAt: now,
	}
	campaign.ClaimedCount++
	s.couponClaims[claim.ID] = claim
	item := cloneUserCoupon(claim)
	item.Coupon = cloneCoupon(campaign)
	return item, nil
}

func (s *Store) Lock(_ context.Context, userID, id, taskID string, now time.Time) (*coupon.UserCoupon, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	claim := s.couponClaims[id]
	if claim == nil || claim.UserID != userID {
		return nil, domain.ErrNotFound
	}
	switch claim.Status {
	case coupon.ClaimUsed, coupon.ClaimExpired:
		return nil, fmt.Errorf("%w: 该券已被使用或已失效", domain.ErrConflict)
	case coupon.ClaimLocked:
		if claim.TaskID != taskID {
			return nil, fmt.Errorf("%w: 该券已锁定在其他订单", domain.ErrConflict)
		}
	case coupon.ClaimUnused:
		claim.Status, claim.TaskID = coupon.ClaimLocked, taskID
	}
	item := cloneUserCoupon(claim)
	item.Coupon = cloneCoupon(s.coupons[claim.CouponID])
	return item, nil
}

func (s *Store) MarkUsed(_ context.Context, id, taskID, paymentOrderID string, discountFen int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	claim := s.couponClaims[id]
	if claim == nil {
		return domain.ErrNotFound
	}
	if claim.Status == coupon.ClaimUsed {
		if claim.TaskID == taskID {
			return nil
		}
		return fmt.Errorf("%w: 该券已被其他订单使用", domain.ErrConflict)
	}
	if claim.Status != coupon.ClaimLocked && claim.Status != coupon.ClaimUnused {
		return fmt.Errorf("%w: 该券当前不可核销", domain.ErrConflict)
	}
	if claim.Status == coupon.ClaimLocked && claim.TaskID != "" && claim.TaskID != taskID {
		return fmt.Errorf("%w: 该券锁定的订单不一致", domain.ErrConflict)
	}
	claim.Status, claim.TaskID, claim.PaymentOrder = coupon.ClaimUsed, taskID, paymentOrderID
	claim.DiscountFen, claim.UsedAt = discountFen, copyTime(&now)
	return nil
}

func (s *Store) Release(_ context.Context, id, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	claim := s.couponClaims[id]
	if claim == nil {
		return domain.ErrNotFound
	}
	if claim.Status == coupon.ClaimLocked && claim.TaskID == taskID {
		claim.Status, claim.TaskID = coupon.ClaimUnused, ""
	}
	return nil
}

func pageCoupons(items []*coupon.Coupon, page repository.Page) []*coupon.Coupon {
	if page.Offset >= len(items) {
		return []*coupon.Coupon{}
	}
	items = items[page.Offset:]
	if page.Limit > 0 && len(items) > page.Limit {
		items = items[:page.Limit]
	}
	return items
}

func pageUserCoupons(items []*coupon.UserCoupon, page repository.Page) []*coupon.UserCoupon {
	if page.Offset >= len(items) {
		return []*coupon.UserCoupon{}
	}
	items = items[page.Offset:]
	if page.Limit > 0 && len(items) > page.Limit {
		items = items[:page.Limit]
	}
	return items
}
