package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/coupon"
)

type couponRecord struct {
	StartsAt            *time.Time `gorm:"column:starts_at"`
	EndsAt              *time.Time `gorm:"column:ends_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
	ApplicableTaskTypes []string   `gorm:"column:applicable_task_types;type:json;serializer:json"`
	ID                  string     `gorm:"column:id;type:char(36);primaryKey"`
	Code                string     `gorm:"column:code;size:32;not null;uniqueIndex:coupons_code"`
	Title               string     `gorm:"column:title;size:120;not null"`
	Subtitle            *string    `gorm:"column:subtitle;size:180"`
	Description         *string    `gorm:"column:description;type:text"`
	Type                string     `gorm:"column:type;size:16;not null"`
	Status              string     `gorm:"column:status;size:16;not null"`
	ValueFen            int64      `gorm:"column:value_fen;not null"`
	DiscountPercent     int        `gorm:"column:discount_percent;not null"`
	MaxDiscountFen      int64      `gorm:"column:max_discount_fen;not null"`
	MinSpendFen         int64      `gorm:"column:min_spend_fen;not null"`
	TotalQuantity       int        `gorm:"column:total_quantity;not null"`
	ClaimedCount        int        `gorm:"column:claimed_count;not null"`
	PerUserLimit        int        `gorm:"column:per_user_limit;not null"`
}

func (couponRecord) TableName() string { return "coupons" }

type couponClaimRecord struct {
	ClaimedAt      time.Time  `gorm:"column:claimed_at"`
	UsedAt         *time.Time `gorm:"column:used_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	ID             string     `gorm:"column:id;type:char(36);primaryKey"`
	CouponID       string     `gorm:"column:coupon_id;type:char(36);not null"`
	UserID         string     `gorm:"column:user_id;type:char(36);not null"`
	Status         string     `gorm:"column:status;size:16;not null"`
	TaskID         *string    `gorm:"column:task_id;type:char(36)"`
	PaymentOrderID *string    `gorm:"column:payment_order_id;type:char(36)"`
	ClaimNo        int        `gorm:"column:claim_no;not null"`
	DiscountFen    int64      `gorm:"column:discount_fen;not null"`
}

func (couponClaimRecord) TableName() string { return "coupon_claims" }

func toCoupon(record couponRecord) *coupon.Coupon {
	remaining := record.TotalQuantity - record.ClaimedCount
	if remaining < 0 {
		remaining = 0
	}
	return &coupon.Coupon{
		ID: record.ID, Code: record.Code, Title: record.Title,
		Subtitle: stringValue(record.Subtitle), Description: stringValue(record.Description),
		Type: record.Type, Status: record.Status,
		ApplicableTaskTypes: append([]string(nil), record.ApplicableTaskTypes...),
		ValueFen:            record.ValueFen, DiscountPercent: record.DiscountPercent,
		MaxDiscountFen: record.MaxDiscountFen, MinSpendFen: record.MinSpendFen,
		TotalQuantity: record.TotalQuantity, ClaimedCount: record.ClaimedCount,
		PerUserLimit: record.PerUserLimit, Remaining: remaining,
		StartsAt: record.StartsAt, EndsAt: record.EndsAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func toUserCoupon(record couponClaimRecord) *coupon.UserCoupon {
	return &coupon.UserCoupon{
		ID: record.ID, CouponID: record.CouponID, UserID: record.UserID,
		ClaimNo: record.ClaimNo, Status: record.Status,
		ClaimedAt: record.ClaimedAt, UsedAt: record.UsedAt,
		TaskID: stringValue(record.TaskID), PaymentOrder: stringValue(record.PaymentOrderID),
		DiscountFen: record.DiscountFen,
	}
}

func (s *Store) ListCoupons(ctx context.Context, userID string, now time.Time, page repository.Page) ([]*coupon.Coupon, error) {
	var records []couponRecord
	query := s.orm.WithContext(ctx).Model(&couponRecord{}).
		Where("status = ?", coupon.StatusActive).
		Where("(starts_at IS NULL OR starts_at <= ?)", now).
		Where("(ends_at IS NULL OR ends_at > ?)", now).
		Order("created_at DESC").Order("id").
		Limit(page.Limit).Offset(page.Offset)
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list coupons: %w", err)
	}
	result := make([]*coupon.Coupon, 0, len(records))
	for _, record := range records {
		result = append(result, toCoupon(record))
	}
	if len(records) == 0 || userID == "" {
		return result, nil
	}
	// The claim center needs to know whether this user already owns a copy and
	// what state it is in. Count every claim, including used/expired ones, so the
	// per-user limit stays visible instead of turning the card back into "领取".
	var claims []couponClaimRecord
	if err := s.orm.WithContext(ctx).Model(&couponClaimRecord{}).
		Select("id, coupon_id, status").
		Where("user_id = ?", userID).
		Where("coupon_id IN ?", couponIDs(result)).
		Order("id").
		Find(&claims).Error; err != nil {
		return nil, fmt.Errorf("list user coupon claims: %w", err)
	}
	byID := make(map[string]struct {
		count  int
		id     string
		status string
	}, len(claims))
	for _, claim := range claims {
		stat := byID[claim.CouponID]
		stat.count++
		if stat.id == "" || claimStatusPriority(claim.Status) > claimStatusPriority(stat.status) {
			stat.id, stat.status = claim.ID, claim.Status
		}
		byID[claim.CouponID] = stat
	}
	for _, item := range result {
		if stat, ok := byID[item.ID]; ok {
			item.UserClaimCount, item.UserCouponID, item.UserCouponStatus = stat.count, stat.id, stat.status
		}
	}
	return result, nil
}

func claimStatusPriority(status string) int {
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

func couponIDs(items []*coupon.Coupon) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func (s *Store) ListUserCoupons(ctx context.Context, userID, status string, page repository.Page) ([]*coupon.UserCoupon, error) {
	var claims []couponClaimRecord
	query := s.orm.WithContext(ctx).Model(&couponClaimRecord{}).
		Where("user_id = ?", userID).
		Order("claimed_at DESC").Order("id").
		Limit(page.Limit).Offset(page.Offset)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&claims).Error; err != nil {
		return nil, fmt.Errorf("list user coupons: %w", err)
	}
	if len(claims) == 0 {
		return []*coupon.UserCoupon{}, nil
	}
	ids := make([]string, 0, len(claims))
	for _, claim := range claims {
		ids = append(ids, claim.CouponID)
	}
	var campaigns []couponRecord
	if err := s.orm.WithContext(ctx).Model(&couponRecord{}).Where("id IN ?", ids).Find(&campaigns).Error; err != nil {
		return nil, fmt.Errorf("load coupons for claims: %w", err)
	}
	byID := make(map[string]*coupon.Coupon, len(campaigns))
	for _, campaign := range campaigns {
		byID[campaign.ID] = toCoupon(campaign)
	}
	result := make([]*coupon.UserCoupon, 0, len(claims))
	for _, claim := range claims {
		item := toUserCoupon(claim)
		item.Coupon = byID[item.CouponID]
		result = append(result, item)
	}
	return result, nil
}

func (s *Store) UserCoupon(ctx context.Context, userID, id string) (*coupon.UserCoupon, error) {
	var claim couponClaimRecord
	result := s.orm.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Limit(1).Find(&claim)
	if result.Error != nil {
		return nil, fmt.Errorf("load user coupon: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	var campaign couponRecord
	if err := s.orm.WithContext(ctx).Where("id = ?", claim.CouponID).Limit(1).Find(&campaign).Error; err != nil {
		return nil, fmt.Errorf("load coupon campaign: %w", err)
	}
	if campaign.ID == "" {
		return nil, domain.ErrNotFound
	}
	item := toUserCoupon(claim)
	item.Coupon = toCoupon(campaign)
	return item, nil
}

func (s *Store) ClaimCoupon(ctx context.Context, userID, couponID string, now time.Time) (*coupon.UserCoupon, error) {
	claimID, err := coupon.NewID()
	if err != nil {
		return nil, err
	}
	var result *coupon.UserCoupon
	err = s.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign couponRecord
		lock := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", couponID).Limit(1).Find(&campaign)
		if lock.Error != nil {
			return fmt.Errorf("lock coupon: %w", lock.Error)
		}
		if lock.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		if campaign.Status != coupon.StatusActive ||
			(campaign.StartsAt != nil && now.Before(*campaign.StartsAt)) ||
			(campaign.EndsAt != nil && !now.Before(*campaign.EndsAt)) {
			return fmt.Errorf("%w: 该券当前不可领取", domain.ErrConflict)
		}
		if campaign.ClaimedCount >= campaign.TotalQuantity {
			return fmt.Errorf("%w: 该券已领完", domain.ErrConflict)
		}
		var count int64
		if err := tx.Model(&couponClaimRecord{}).Where("coupon_id = ? AND user_id = ?", couponID, userID).Count(&count).Error; err != nil {
			return fmt.Errorf("count user claims: %w", err)
		}
		if count >= int64(campaign.PerUserLimit) {
			return fmt.Errorf("%w: 你已达到该券的领取上限", domain.ErrConflict)
		}
		record := couponClaimRecord{
			ID: claimID, CouponID: couponID, UserID: userID,
			ClaimNo: int(count) + 1, Status: coupon.ClaimUnused, ClaimedAt: now,
			DiscountFen: 0, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return fmt.Errorf("%w: 该券已被领取", domain.ErrConflict)
			}
			return fmt.Errorf("insert coupon claim: %w", err)
		}
		if err := tx.Model(&couponRecord{}).Where("id = ?", couponID).
			Update("claimed_count", gorm.Expr("claimed_count + 1")).Error; err != nil {
			return fmt.Errorf("increment coupon claimed count: %w", err)
		}
		campaign.ClaimedCount++
		item := toUserCoupon(record)
		item.Coupon = toCoupon(campaign)
		result = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) Lock(ctx context.Context, userID, id, taskID string, now time.Time) (*coupon.UserCoupon, error) {
	var result *coupon.UserCoupon
	err := s.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var claim couponClaimRecord
		lock := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", id, userID).Limit(1).Find(&claim)
		if lock.Error != nil {
			return fmt.Errorf("lock coupon claim: %w", lock.Error)
		}
		if lock.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		switch claim.Status {
		case coupon.ClaimUsed, coupon.ClaimExpired:
			return fmt.Errorf("%w: 该券已被使用或已失效", domain.ErrConflict)
		case coupon.ClaimLocked:
			if stringValue(claim.TaskID) != taskID {
				return fmt.Errorf("%w: 该券已锁定在其他订单", domain.ErrConflict)
			}
		case coupon.ClaimUnused:
			claim.Status, claim.TaskID = coupon.ClaimLocked, &taskID
			claim.UpdatedAt = now
			if err := tx.Model(&couponClaimRecord{}).Where("id = ? AND status = ?", claim.ID, coupon.ClaimUnused).
				Updates(map[string]any{"status": coupon.ClaimLocked, "task_id": taskID, "updated_at": now}).Error; err != nil {
				return fmt.Errorf("lock coupon for task: %w", err)
			}
		}
		var campaign couponRecord
		if err := tx.Where("id = ?", claim.CouponID).Limit(1).Find(&campaign).Error; err != nil {
			return fmt.Errorf("load coupon campaign: %w", err)
		}
		if campaign.ID == "" {
			return domain.ErrNotFound
		}
		item := toUserCoupon(claim)
		item.Coupon = toCoupon(campaign)
		result = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) MarkUsed(ctx context.Context, id, taskID, paymentOrderID string, discountFen int64, now time.Time) error {
	var claim couponClaimRecord
	result := s.orm.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&claim)
	if result.Error != nil {
		return fmt.Errorf("load coupon claim for use: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	if claim.Status == coupon.ClaimUsed {
		if stringValue(claim.TaskID) == taskID {
			return nil
		}
		return fmt.Errorf("%w: 该券已被其他订单使用", domain.ErrConflict)
	}
	if claim.Status != coupon.ClaimLocked && claim.Status != coupon.ClaimUnused {
		return fmt.Errorf("%w: 该券当前不可核销", domain.ErrConflict)
	}
	if claim.Status == coupon.ClaimLocked && stringValue(claim.TaskID) != "" && stringValue(claim.TaskID) != taskID {
		return fmt.Errorf("%w: 该券锁定的订单不一致", domain.ErrConflict)
	}
	result = s.orm.WithContext(ctx).Model(&couponClaimRecord{}).
		Where("id = ? AND status IN ?", id, []string{coupon.ClaimUnused, coupon.ClaimLocked}).
		Updates(map[string]any{
			"status": coupon.ClaimUsed, "task_id": taskID,
			"payment_order_id": paymentOrderID, "discount_fen": discountFen,
			"used_at": now, "updated_at": now,
		})
	if result.Error != nil {
		return fmt.Errorf("mark coupon used: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 优惠券状态已变化", domain.ErrConflict)
	}
	return nil
}

func (s *Store) Release(ctx context.Context, id, taskID string) error {
	result := s.orm.WithContext(ctx).Model(&couponClaimRecord{}).
		Where("id = ? AND status = ? AND task_id = ?", id, coupon.ClaimLocked, taskID).
		Updates(map[string]any{"status": coupon.ClaimUnused, "task_id": nil, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("release coupon: %w", result.Error)
	}
	return nil
}
