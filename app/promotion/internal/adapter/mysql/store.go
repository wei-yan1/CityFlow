package promotionadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/app/promotion/internal/application/promotionapp"
	"cityflow/app/promotion/internal/domain/coupon"
	platformmysql "cityflow/internal/platform/mysql"
)

type Store struct{ db *platformmysql.DB }

func New(db *platformmysql.DB) *Store { return &Store{db: db} }

var _ promotionapp.Store = (*Store)(nil)

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
	IdempotencyKey *string    `gorm:"column:idempotency_key;size:64"`
}

func (couponClaimRecord) TableName() string { return "coupon_claims" }

const redeemCodeStatusActive = "ACTIVE"

// redeemCodeRecord is the batch code row. max_redeem is the total number of
// users who may redeem this code; used_count is advanced atomically. seq is
// the database-allocated serial encoded into the visible Base32 code.
type redeemCodeRecord struct {
	StartsAt  *time.Time `gorm:"column:starts_at"`
	EndsAt    *time.Time `gorm:"column:ends_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
	ID        string     `gorm:"column:id;type:char(36);primaryKey"`
	CouponID  string     `gorm:"column:coupon_id;type:char(36);not null"`
	BatchNo   string     `gorm:"column:batch_no;size:64;not null"`
	Code      string     `gorm:"column:code;size:16;not null;uniqueIndex:uk_redeem_code"`
	Seq       *uint64    `gorm:"column:seq;uniqueIndex:uk_redeem_seq"`
	Status    string     `gorm:"column:status;size:16;not null"`
	CreatedBy string     `gorm:"column:created_by;size:64;not null"`
	Note      *string    `gorm:"column:note;size:255"`
	MaxRedeem int        `gorm:"column:max_redeem;not null"`
	UsedCount int        `gorm:"column:used_count;not null"`
}

func (redeemCodeRecord) TableName() string { return "coupon_redeem_code" }

type redeemSequenceRecord struct {
	Name      string `gorm:"column:name;primaryKey"`
	NextValue uint64 `gorm:"column:next_value;not null"`
}

func (redeemSequenceRecord) TableName() string { return "promotion_redeem_code_sequence" }

// redeemRecord is the per-user redemption fact. uk_redeem_code_user is the
// final "one user, one code" guard; the transaction alone is not enough.
type redeemRecord struct {
	CreatedAt time.Time `gorm:"column:created_at"`
	ID        string    `gorm:"column:id;type:char(36);primaryKey"`
	CodeID    string    `gorm:"column:code_id;type:char(36);not null"`
	UserID    string    `gorm:"column:user_id;type:char(36);not null"`
	CouponID  string    `gorm:"column:coupon_id;type:char(36);not null"`
	ClaimID   string    `gorm:"column:claim_id;type:char(36);not null"`
}

func (redeemRecord) TableName() string { return "coupon_redeem_record" }

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

func toRedeemCode(record redeemCodeRecord) *coupon.RedeemCode {
	var seq uint64
	if record.Seq != nil {
		seq = *record.Seq
	}
	return &coupon.RedeemCode{
		ID: record.ID, CouponID: record.CouponID, BatchNo: record.BatchNo,
		Code: record.Code, Seq: seq, MaxRedeem: record.MaxRedeem,
		UsedCount: record.UsedCount, Status: record.Status, CreatedBy: record.CreatedBy,
		Note: stringValue(record.Note), StartsAt: record.StartsAt, EndsAt: record.EndsAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func (s *Store) ListCoupons(ctx context.Context, userID string, now time.Time, page promotionapp.Page) ([]*coupon.Coupon, error) {
	var records []couponRecord
	query := s.db.ORM.WithContext(ctx).Model(&couponRecord{}).
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
	if err := s.db.ORM.WithContext(ctx).Model(&couponClaimRecord{}).
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

func (s *Store) ListUserCoupons(ctx context.Context, userID, status string, page promotionapp.Page) ([]*coupon.UserCoupon, error) {
	var claims []couponClaimRecord
	query := s.db.ORM.WithContext(ctx).Model(&couponClaimRecord{}).
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
	if err := s.db.ORM.WithContext(ctx).Model(&couponRecord{}).Where("id IN ?", ids).Find(&campaigns).Error; err != nil {
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

// Coupon 返回单张券的完整规则快照，供缓存层回源。
func (s *Store) Coupon(ctx context.Context, id string) (*coupon.Coupon, error) {
	var record couponRecord
	result := s.db.ORM.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&record)
	if result.Error != nil {
		return nil, fmt.Errorf("load coupon: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, coupon.ErrNotFound
	}
	return toCoupon(record), nil
}

func (s *Store) UserCoupon(ctx context.Context, userID, id string) (*coupon.UserCoupon, error) {
	var claim couponClaimRecord
	result := s.db.ORM.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Limit(1).Find(&claim)
	if result.Error != nil {
		return nil, fmt.Errorf("load user coupon: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, coupon.ErrNotFound
	}
	var campaign couponRecord
	if err := s.db.ORM.WithContext(ctx).Where("id = ?", claim.CouponID).Limit(1).Find(&campaign).Error; err != nil {
		return nil, fmt.Errorf("load coupon campaign: %w", err)
	}
	if campaign.ID == "" {
		return nil, coupon.ErrNotFound
	}
	item := toUserCoupon(claim)
	item.Coupon = toCoupon(campaign)
	return item, nil
}

func (s *Store) ClaimCoupon(ctx context.Context, userID, couponID, idempotencyKey string, now time.Time) (*coupon.UserCoupon, error) {
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return nil, err
	}
	if key != "" {
		if item, lookupErr := s.ClaimByIdempotency(ctx, userID, couponID, key); lookupErr == nil {
			return item, nil
		} else if !errors.Is(lookupErr, coupon.ErrNotFound) {
			return nil, lookupErr
		}
	}

	claimID, err := coupon.NewID()
	if err != nil {
		return nil, err
	}
	var result *coupon.UserCoupon
	err = s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign couponRecord
		// 非锁定读取只用于业务校验和构造返回快照；真正的库存闸门是下面的条件 UPDATE。
		if err := tx.Where("id = ?", couponID).Limit(1).Find(&campaign).Error; err != nil {
			return fmt.Errorf("load coupon: %w", err)
		}
		if campaign.ID == "" {
			return coupon.ErrNotFound
		}
		if campaign.Status != coupon.StatusActive ||
			(campaign.StartsAt != nil && now.Before(*campaign.StartsAt)) ||
			(campaign.EndsAt != nil && !now.Before(*campaign.EndsAt)) {
			return fmt.Errorf("%w: 该券当前不可领取", coupon.ErrConflict)
		}
		// 一条条件 UPDATE 同时完成“校验 + 扣库存”，并持有该行锁到事务提交。
		// 比先 SELECT ... FOR UPDATE 再 UPDATE 少一次往返，锁窗口也更短。
		update := tx.Model(&couponRecord{}).
			Where("id = ? AND status = ? AND claimed_count < total_quantity", couponID, coupon.StatusActive).
			Where("(starts_at IS NULL OR starts_at <= ?)", now).
			Where("(ends_at IS NULL OR ends_at > ?)", now).
			Update("claimed_count", gorm.Expr("claimed_count + 1"))
		if update.Error != nil {
			return fmt.Errorf("reserve coupon stock: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			return fmt.Errorf("%w: 该券已领完", coupon.ErrConflict)
		}
		var existing []couponClaimRecord
		// 必须是锁定读（当前读）。REPEATABLE READ 下普通 COUNT 沿用事务开始时的
		// 快照，看不到并发事务刚提交的 claim，会把 claim_no 算重、撞唯一键。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "claim_no").
			Where("coupon_id = ? AND user_id = ?", couponID, userID).
			Find(&existing).Error; err != nil {
			return fmt.Errorf("load user claims: %w", err)
		}
		limit := campaign.PerUserLimit
		if limit <= 0 {
			limit = 1
		}
		if int64(len(existing)) >= int64(limit) {
			return fmt.Errorf("%w: 你已达到该券的领取上限", coupon.ErrConflict)
		}
		claimNo := 1
		for _, record := range existing {
			if record.ClaimNo >= claimNo {
				claimNo = record.ClaimNo + 1
			}
		}
		record := couponClaimRecord{
			ID: claimID, CouponID: couponID, UserID: userID,
			ClaimNo: claimNo, Status: coupon.ClaimUnused, ClaimedAt: now,
			DiscountFen: 0, CreatedAt: now, UpdatedAt: now,
		}
		if key != "" {
			record.IdempotencyKey = &key
		}
		if err := tx.Create(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return errClaimDuplicate
			}
			return fmt.Errorf("insert coupon claim: %w", err)
		}
		campaign.ClaimedCount++
		item := toUserCoupon(record)
		item.Coupon = toCoupon(campaign)
		result = item
		return nil
	})
	if err != nil {
		// 并发同键请求：输的一方唯一键冲突或撞上限后回滚库存，再按幂等键回读赢家的 claim。
		if key != "" && (errors.Is(err, errClaimDuplicate) || errors.Is(err, coupon.ErrConflict)) {
			if item, lookupErr := s.ClaimByIdempotency(ctx, userID, couponID, key); lookupErr == nil {
				return item, nil
			}
		}
		if errors.Is(err, errClaimDuplicate) {
			return nil, fmt.Errorf("%w: 该券已被领取", coupon.ErrConflict)
		}
		return nil, err
	}
	return result, nil
}

// ClaimCouponBatch 在一个事务里处理同一张券的一批请求：
// 只锁一次 coupons 行、只做一次库存自增、批量插入 claims。
// 调用方必须先把请求按 coupon 分组；跨券批量会直接拒绝，避免部分成功、部分失败。
func (s *Store) ClaimCouponBatch(ctx context.Context, requests []promotionapp.ClaimRequest, now time.Time) ([]promotionapp.ClaimResult, error) {
	results := make([]promotionapp.ClaimResult, len(requests))
	if len(requests) == 0 {
		return results, nil
	}
	normalized := make([]string, len(requests))
	groups := make(map[string][]int)
	aliases := make(map[int]int)
	seen := make(map[string]int)
	for index, request := range requests {
		userID := strings.TrimSpace(request.UserID)
		couponID := strings.TrimSpace(request.CouponID)
		if userID == "" || couponID == "" {
			results[index].Err = fmt.Errorf("%w: user and coupon ids are required", coupon.ErrInvalidRequest)
			continue
		}
		key, err := normalizeIdempotencyKey(request.IdempotencyKey)
		if err != nil {
			results[index].Err = err
			continue
		}
		normalized[index] = key
		if key != "" {
			tuple := userID + "\x00" + couponID + "\x00" + key
			if first, ok := seen[tuple]; ok {
				aliases[index] = first
				continue
			}
			seen[tuple] = index
		}
		groups[couponID] = append(groups[couponID], index)
	}
	if len(groups) > 1 {
		return nil, fmt.Errorf("%w: batch requests must share one coupon", coupon.ErrInvalidRequest)
	}
	for couponID, indexes := range groups {
		if err := s.claimCouponBatchGroup(ctx, couponID, indexes, requests, normalized, results, now); err != nil {
			return nil, err
		}
	}
	for alias, first := range aliases {
		results[alias] = results[first]
	}
	return results, nil
}

type claimUserState struct {
	Count   int64
	Pending int64
	MaxNo   int
}

func (s *Store) claimCouponBatchGroup(
	ctx context.Context,
	couponID string,
	indexes []int,
	requests []promotionapp.ClaimRequest,
	normalized []string,
	results []promotionapp.ClaimResult,
	now time.Time,
) error {
	var campaign couponRecord
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先锁券行，锁顺序和单条 ClaimCoupon 一致：券 -> claim。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", couponID).Limit(1).Find(&campaign).Error; err != nil {
			return fmt.Errorf("load coupon for batch: %w", err)
		}
		if campaign.ID == "" {
			for _, index := range indexes {
				results[index].Err = coupon.ErrNotFound
			}
			return nil
		}

		users := make([]string, 0, len(indexes))
		userSeen := make(map[string]struct{}, len(indexes))
		for _, index := range indexes {
			userID := strings.TrimSpace(requests[index].UserID)
			if _, ok := userSeen[userID]; !ok {
				userSeen[userID] = struct{}{}
				users = append(users, userID)
			}
		}
		var existing []couponClaimRecord
		// 当前读：算限领、claim_no 和幂等回读必须看到并发已提交的 claim。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("coupon_id = ? AND user_id IN ?", couponID, users).
			Find(&existing).Error; err != nil {
			return fmt.Errorf("load batch user claims: %w", err)
		}
		states := make(map[string]*claimUserState, len(users))
		idempotent := make(map[string]*couponClaimRecord)
		for index := range existing {
			claim := &existing[index]
			state := states[claim.UserID]
			if state == nil {
				state = &claimUserState{}
				states[claim.UserID] = state
			}
			state.Count++
			if claim.ClaimNo > state.MaxNo {
				state.MaxNo = claim.ClaimNo
			}
			if claim.IdempotencyKey != nil && *claim.IdempotencyKey != "" {
				idempotent[claim.UserID+"\x00"+*claim.IdempotencyKey] = claim
			}
		}

		remaining := make([]int, 0, len(indexes))
		for _, index := range indexes {
			userID := strings.TrimSpace(requests[index].UserID)
			key := normalized[index]
			if key != "" {
				if claim, ok := idempotent[userID+"\x00"+key]; ok {
					item := toUserCoupon(*claim)
					item.Coupon = toCoupon(campaign)
					results[index].Item = item
					continue
				}
			}
			remaining = append(remaining, index)
		}
		if len(remaining) == 0 {
			return nil
		}

		if campaign.Status != coupon.StatusActive ||
			(campaign.StartsAt != nil && now.Before(*campaign.StartsAt)) ||
			(campaign.EndsAt != nil && !now.Before(*campaign.EndsAt)) {
			for _, index := range remaining {
				results[index].Err = fmt.Errorf("%w: 该券当前不可领取", coupon.ErrConflict)
			}
			return nil
		}

		limit := campaign.PerUserLimit
		if limit <= 0 {
			limit = 1
		}
		type candidate struct {
			index  int
			userID string
		}
		candidates := make([]candidate, 0, len(remaining))
		for _, index := range remaining {
			userID := strings.TrimSpace(requests[index].UserID)
			state := states[userID]
			if state == nil {
				state = &claimUserState{}
				states[userID] = state
			}
			if state.Count+state.Pending >= int64(limit) {
				results[index].Err = fmt.Errorf("%w: 你已达到该券的领取上限", coupon.ErrConflict)
				continue
			}
			// 同一批里同一用户可能有多个不同幂等键。候选阶段先占住名额，
			// 否则两个候选都看到旧 Count，最终可能一起入选而超限。
			state.Pending++
			candidates = append(candidates, candidate{index: index, userID: userID})
		}

		available := campaign.TotalQuantity - campaign.ClaimedCount
		if available < 0 {
			available = 0
		}
		admitted := len(candidates)
		if admitted > available {
			admitted = available
		}
		if admitted == 0 {
			for _, item := range candidates {
				results[item.index].Err = fmt.Errorf("%w: 该券已领完", coupon.ErrConflict)
			}
			return nil
		}

		update := tx.Model(&couponRecord{}).
			Where("id = ? AND claimed_count + ? <= total_quantity", couponID, admitted).
			Update("claimed_count", gorm.Expr("claimed_count + ?", admitted))
		if update.Error != nil {
			return fmt.Errorf("reserve coupon stock batch: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			for _, item := range candidates {
				results[item.index].Err = fmt.Errorf("%w: 该券已领完", coupon.ErrConflict)
			}
			return nil
		}

		records := make([]couponClaimRecord, 0, admitted)
		for index := 0; index < admitted; index++ {
			item := candidates[index]
			state := states[item.userID]
			state.Pending--
			state.Count++
			state.MaxNo++
			claimID, idErr := coupon.NewID()
			if idErr != nil {
				return idErr
			}
			key := normalized[item.index]
			record := couponClaimRecord{
				ID: claimID, CouponID: couponID, UserID: item.userID,
				ClaimNo: state.MaxNo, Status: coupon.ClaimUnused, ClaimedAt: now,
				DiscountFen: 0, CreatedAt: now, UpdatedAt: now,
			}
			if key != "" {
				value := key
				record.IdempotencyKey = &value
			}
			records = append(records, record)
		}
		if err := tx.CreateInBatches(&records, len(records)).Error; err != nil {
			return fmt.Errorf("insert coupon claims batch: %w", err)
		}
		campaign.ClaimedCount += admitted
		for index := range records {
			item := toUserCoupon(records[index])
			item.Coupon = toCoupon(campaign)
			results[candidates[index].index].Item = item
		}
		for _, item := range candidates[admitted:] {
			results[item.index].Err = fmt.Errorf("%w: 该券已领完", coupon.ErrConflict)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// ClaimByIdempotency 按 (coupon, user, idempotency_key) 回读已经落库的领取结果。
func (s *Store) ClaimByIdempotency(ctx context.Context, userID, couponID, idempotencyKey string) (*coupon.UserCoupon, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return nil, coupon.ErrNotFound
	}
	var claim couponClaimRecord
	res := s.db.ORM.WithContext(ctx).
		Where("coupon_id = ? AND user_id = ? AND idempotency_key = ?", couponID, userID, key).
		Limit(1).Find(&claim)
	if res.Error != nil {
		return nil, fmt.Errorf("load idempotent coupon claim: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, coupon.ErrNotFound
	}
	var campaign couponRecord
	if err := s.db.ORM.WithContext(ctx).Where("id = ?", couponID).Limit(1).Find(&campaign).Error; err != nil {
		return nil, fmt.Errorf("load coupon campaign: %w", err)
	}
	item := toUserCoupon(claim)
	if campaign.ID != "" {
		item.Coupon = toCoupon(campaign)
	}
	return item, nil
}

var errClaimDuplicate = errors.New("coupon claim duplicate")

func normalizeIdempotencyKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil
	}
	if len(key) > 64 {
		return "", fmt.Errorf("%w: 幂等键不能超过 64 个字符", coupon.ErrInvalidRequest)
	}
	return key, nil
}

func (s *Store) RedeemCode(ctx context.Context, userID, code string, now time.Time) (*coupon.UserCoupon, error) {
	var result *coupon.UserCoupon
	var redeemedCodeID string
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var codeRecord redeemCodeRecord
		find := tx.Where("code = ?", code).Limit(1).Find(&codeRecord)
		if find.Error != nil {
			return fmt.Errorf("load redeem code: %w", find.Error)
		}
		if find.RowsAffected == 0 {
			return coupon.ErrNotFound
		}
		redeemedCodeID = codeRecord.ID

		var campaign couponRecord
		if err := tx.Where("id = ?", codeRecord.CouponID).Limit(1).Find(&campaign).Error; err != nil {
			return fmt.Errorf("load redeem coupon: %w", err)
		}
		if campaign.ID == "" {
			return coupon.ErrNotFound
		}

		// Idempotent replay: the same user and code already produced a claim.
		// Return it without consuming another code seat.
		var existing redeemRecord
		if err := tx.Where("code_id = ? AND user_id = ?", codeRecord.ID, userID).Limit(1).Find(&existing).Error; err != nil {
			return fmt.Errorf("load redeem record: %w", err)
		}
		if existing.ID != "" {
			var claim couponClaimRecord
			if err := tx.Where("id = ?", existing.ClaimID).Limit(1).Find(&claim).Error; err != nil {
				return fmt.Errorf("load redeemed claim: %w", err)
			}
			if claim.ID == "" {
				return coupon.ErrNotFound
			}
			item := toUserCoupon(claim)
			item.Coupon = toCoupon(campaign)
			result = item
			return nil
		}

		couponUpdate := tx.Model(&couponRecord{}).
			Where("id = ? AND status = ? AND claimed_count < total_quantity", codeRecord.CouponID, coupon.StatusActive).
			Where("(starts_at IS NULL OR starts_at <= ?)", now).
			Where("(ends_at IS NULL OR ends_at > ?)", now).
			Update("claimed_count", gorm.Expr("claimed_count + 1"))
		if couponUpdate.Error != nil {
			return fmt.Errorf("reserve redeem coupon stock: %w", couponUpdate.Error)
		}
		if couponUpdate.RowsAffected == 0 {
			return fmt.Errorf("%w: 优惠券已领完或当前不可领取", coupon.ErrConflict)
		}

		codeUpdate := tx.Model(&redeemCodeRecord{}).
			Where("id = ? AND status = ? AND used_count < max_redeem", codeRecord.ID, redeemCodeStatusActive).
			Where("(starts_at IS NULL OR starts_at <= ?)", now).
			Where("(ends_at IS NULL OR ends_at > ?)", now).
			Update("used_count", gorm.Expr("used_count + 1"))
		if codeUpdate.Error != nil {
			return fmt.Errorf("reserve redeem code seat: %w", codeUpdate.Error)
		}
		if codeUpdate.RowsAffected == 0 {
			return fmt.Errorf("%w: 兑换码已用完或已停用", coupon.ErrConflict)
		}

		var userClaims []couponClaimRecord
		// 与 ClaimCoupon 同理：兑换码路径也可能同一用户并发兑换不同码，
		// 普通 COUNT 是快照读，必须改成当前读，否则 claim_no 会撞唯一键。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "claim_no").
			Where("coupon_id = ? AND user_id = ?", codeRecord.CouponID, userID).
			Find(&userClaims).Error; err != nil {
			return fmt.Errorf("load coupon claims: %w", err)
		}
		perUserLimit := campaign.PerUserLimit
		if perUserLimit <= 0 {
			perUserLimit = 1
		}
		if int64(len(userClaims)) >= int64(perUserLimit) {
			return fmt.Errorf("%w: 你已达到该券的领取上限", coupon.ErrConflict)
		}
		claimNo := 1
		for _, record := range userClaims {
			if record.ClaimNo >= claimNo {
				claimNo = record.ClaimNo + 1
			}
		}
		claimID, err := coupon.NewID()
		if err != nil {
			return err
		}
		redeemID, err := coupon.NewID()
		if err != nil {
			return err
		}
		claim := couponClaimRecord{
			ID: claimID, CouponID: codeRecord.CouponID, UserID: userID,
			ClaimNo: claimNo, Status: coupon.ClaimUnused,
			ClaimedAt: now, CreatedAt: now, UpdatedAt: now, DiscountFen: 0,
		}
		if err := tx.Create(&claim).Error; err != nil {
			return fmt.Errorf("insert redeemed coupon claim: %w", err)
		}
		if err := tx.Create(&redeemRecord{
			ID: redeemID, CodeID: codeRecord.ID, UserID: userID,
			CouponID: codeRecord.CouponID, ClaimID: claimID, CreatedAt: now,
		}).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return fmt.Errorf("%w: 该兑换码已兑换过", coupon.ErrConflict)
			}
			return fmt.Errorf("insert redeem record: %w", err)
		}

		campaign.ClaimedCount++
		item := toUserCoupon(claim)
		item.Coupon = toCoupon(campaign)
		result = item
		return nil
	})
	if err != nil {
		// A concurrent duplicate may lose the code-seat CAS before it can insert
		// the redeem record. Re-read by the unique key so the losing request
		// returns the winner's claim instead of a spurious conflict.
		if errors.Is(err, coupon.ErrConflict) && redeemedCodeID != "" {
			var existing redeemRecord
			if findErr := s.db.ORM.WithContext(ctx).
				Where("code_id = ? AND user_id = ?", redeemedCodeID, userID).
				Limit(1).Find(&existing).Error; findErr == nil && existing.ID != "" {
				var claim couponClaimRecord
				if claimErr := s.db.ORM.WithContext(ctx).Where("id = ?", existing.ClaimID).
					Limit(1).Find(&claim).Error; claimErr == nil && claim.ID != "" {
					var campaign couponRecord
					if campaignErr := s.db.ORM.WithContext(ctx).Where("id = ?", claim.CouponID).
						Limit(1).Find(&campaign).Error; campaignErr == nil && campaign.ID != "" {
						item := toUserCoupon(claim)
						item.Coupon = toCoupon(campaign)
						return item, nil
					}
				}
			}
		}
		return nil, err
	}
	return result, nil
}

func (s *Store) GenerateRedeemCodes(ctx context.Context, input coupon.GenerateRedeemCodesInput, now time.Time) ([]*coupon.RedeemCode, error) {
	var result []*coupon.RedeemCode
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign couponRecord
		// Serialize generation for one coupon so a retry with the same batch_no
		// either sees the committed batch or creates exactly one new batch.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", input.CouponID).Limit(1).Find(&campaign).Error; err != nil {
			return fmt.Errorf("load redeem coupon: %w", err)
		}
		if campaign.ID == "" {
			return coupon.ErrNotFound
		}
		if campaign.Status != coupon.StatusActive {
			return fmt.Errorf("%w: 该券当前不可发码", coupon.ErrConflict)
		}
		if input.MaxRedeem > campaign.TotalQuantity {
			return fmt.Errorf("%w: 单码可兑换人数不能超过券库存", coupon.ErrInvalidRequest)
		}
		if input.BatchNo != "" {
			var existing []redeemCodeRecord
			if err := tx.Where("coupon_id = ? AND batch_no = ?", input.CouponID, input.BatchNo).
				Order("seq").Find(&existing).Error; err != nil {
				return fmt.Errorf("load existing redeem batch: %w", err)
			}
			if len(existing) > 0 {
				result = make([]*coupon.RedeemCode, 0, len(existing))
				for _, record := range existing {
					result = append(result, toRedeemCode(record))
				}
				return nil
			}
		}

		startSeq, err := allocateRedeemSequence(tx, uint64(input.Count))
		if err != nil {
			return err
		}
		batchNo := input.BatchNo
		if batchNo == "" {
			batchNo = fmt.Sprintf("B%s-%d", now.UTC().Format("20060102"), startSeq)
		}
		records := make([]redeemCodeRecord, 0, input.Count)
		for index := 0; index < input.Count; index++ {
			seq := startSeq + uint64(index)
			code, err := coupon.GenerateRedeemCode(seq, input.CouponID)
			if err != nil {
				return err
			}
			id, err := coupon.NewID()
			if err != nil {
				return err
			}
			var note *string
			if input.Note != "" {
				value := input.Note
				note = &value
			}
			seqValue := seq
			records = append(records, redeemCodeRecord{
				ID: id, CouponID: input.CouponID, BatchNo: batchNo, Code: code, Seq: &seqValue,
				MaxRedeem: input.MaxRedeem, UsedCount: 0, Status: coupon.RedeemStatusActive,
				CreatedBy: input.CreatedBy, Note: note, StartsAt: input.StartsAt, EndsAt: input.EndsAt,
				CreatedAt: now, UpdatedAt: now,
			})
		}
		if err := tx.CreateInBatches(&records, 200).Error; err != nil {
			return fmt.Errorf("insert redeem codes: %w", err)
		}
		result = make([]*coupon.RedeemCode, 0, len(records))
		for _, record := range records {
			result = append(result, toRedeemCode(record))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

const redeemSequenceName = "coupon_redeem_code"

func allocateRedeemSequence(tx *gorm.DB, count uint64) (uint64, error) {
	if count == 0 {
		return 0, fmt.Errorf("%w: redeem code count is required", coupon.ErrInvalidRequest)
	}
	var sequence redeemSequenceRecord
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("name = ?", redeemSequenceName).Take(&sequence).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		sequence = redeemSequenceRecord{Name: redeemSequenceName, NextValue: 1}
		if err := tx.Create(&sequence).Error; err != nil {
			return 0, fmt.Errorf("create redeem sequence: %w", err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("lock redeem sequence: %w", err)
	}
	start := sequence.NextValue
	if start == 0 {
		start = 1
	}
	if start > 0xFFFFFFFF || count > 0xFFFFFFFF-start+1 {
		return 0, fmt.Errorf("%w: 兑换码序列号已用尽", coupon.ErrConflict)
	}
	if err := tx.Model(&redeemSequenceRecord{}).Where("name = ?", redeemSequenceName).
		Update("next_value", start+count).Error; err != nil {
		return 0, fmt.Errorf("advance redeem sequence: %w", err)
	}
	return start, nil
}

func (s *Store) ListRedeemCodes(ctx context.Context, filter coupon.RedeemCodeFilter, page promotionapp.Page) ([]*coupon.RedeemCode, error) {
	var records []redeemCodeRecord
	query := s.db.ORM.WithContext(ctx).Model(&redeemCodeRecord{})
	if filter.CouponID != "" {
		query = query.Where("coupon_id = ?", filter.CouponID)
	}
	if filter.BatchNo != "" {
		query = query.Where("batch_no = ?", filter.BatchNo)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if err := query.Order("created_at DESC").Order("seq DESC").
		Limit(page.Limit).Offset(page.Offset).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list redeem codes: %w", err)
	}
	result := make([]*coupon.RedeemCode, 0, len(records))
	for _, record := range records {
		result = append(result, toRedeemCode(record))
	}
	return result, nil
}

func (s *Store) DisableRedeemCode(ctx context.Context, id string, now time.Time) error {
	result := s.db.ORM.WithContext(ctx).Model(&redeemCodeRecord{}).
		Where("id = ? AND status = ?", id, coupon.RedeemStatusActive).
		Update("status", coupon.RedeemStatusDisabled)
	if result.Error != nil {
		return fmt.Errorf("disable redeem code: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var count int64
	if err := s.db.ORM.WithContext(ctx).Model(&redeemCodeRecord{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("check redeem code: %w", err)
	}
	if count == 0 {
		return coupon.ErrNotFound
	}
	return nil
}

func (s *Store) Lock(ctx context.Context, userID, id, taskID string, now time.Time) (*coupon.UserCoupon, error) {
	var result *coupon.UserCoupon
	err := s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var claim couponClaimRecord
		lock := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", id, userID).Limit(1).Find(&claim)
		if lock.Error != nil {
			return fmt.Errorf("lock coupon claim: %w", lock.Error)
		}
		if lock.RowsAffected == 0 {
			return coupon.ErrNotFound
		}
		switch claim.Status {
		case coupon.ClaimUsed, coupon.ClaimExpired:
			return fmt.Errorf("%w: 该券已被使用或已失效", coupon.ErrConflict)
		case coupon.ClaimLocked:
			if stringValue(claim.TaskID) != taskID {
				return fmt.Errorf("%w: 该券已锁定在其他任务", coupon.ErrConflict)
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
			return coupon.ErrNotFound
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
	result := s.db.ORM.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&claim)
	if result.Error != nil {
		return fmt.Errorf("load coupon claim for use: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return coupon.ErrNotFound
	}
	if claim.Status == coupon.ClaimUsed {
		if stringValue(claim.TaskID) == taskID {
			return nil
		}
		return fmt.Errorf("%w: 该券已被其他任务使用", coupon.ErrConflict)
	}
	if claim.Status != coupon.ClaimLocked && claim.Status != coupon.ClaimUnused {
		return fmt.Errorf("%w: 该券当前不可核销", coupon.ErrConflict)
	}
	if claim.Status == coupon.ClaimLocked && stringValue(claim.TaskID) != "" && stringValue(claim.TaskID) != taskID {
		return fmt.Errorf("%w: 该券锁定的任务不一致", coupon.ErrConflict)
	}
	result = s.db.ORM.WithContext(ctx).Model(&couponClaimRecord{}).
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
		return fmt.Errorf("%w: 优惠券状态已变化", coupon.ErrConflict)
	}
	return nil
}

func (s *Store) Release(ctx context.Context, id, taskID string) error {
	result := s.db.ORM.WithContext(ctx).Model(&couponClaimRecord{}).
		Where("id = ? AND status = ? AND task_id = ?", id, coupon.ClaimLocked, taskID).
		Updates(map[string]any{"status": coupon.ClaimUnused, "task_id": nil, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("release coupon: %w", result.Error)
	}
	return nil
}

// ReleaseStaleLocked returns claims whose checkout never reached payment.
// Seven days is deliberately longer than any provider expiry window, so a
// legitimate late callback is not racing this cleanup.
func (s *Store) ReleaseStaleLocked(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := s.db.ORM.WithContext(ctx).Exec(
		"UPDATE coupon_claims SET status = ?, task_id = NULL, payment_order_id = NULL, updated_at = ? "+
			"WHERE status = ? AND updated_at < ? LIMIT ?",
		coupon.ClaimUnused, time.Now().UTC(), coupon.ClaimLocked, before, limit,
	)
	if result.Error != nil {
		return 0, fmt.Errorf("release stale coupon claims: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// ExpireClaims makes the read-time Mine projection durable. The predicate
// matches Coupon.ActiveAt: not ACTIVE, not started, or already ended.
func (s *Store) ExpireClaims(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := s.db.ORM.WithContext(ctx).Exec(
		"UPDATE coupon_claims SET status = ?, updated_at = ? "+
			"WHERE status = ? AND coupon_id IN ("+
			"SELECT id FROM coupons WHERE status <> ? OR (starts_at IS NOT NULL AND starts_at > ?) OR (ends_at IS NOT NULL AND ends_at <= ?)"+
			") LIMIT ?",
		coupon.ClaimExpired, time.Now().UTC(), coupon.ClaimUnused, coupon.StatusActive, now, now, limit,
	)
	if result.Error != nil {
		return 0, fmt.Errorf("expire coupon claims: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}


