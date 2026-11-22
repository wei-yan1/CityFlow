package promotionapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"cityflow/app/promotion/internal/domain/coupon"
)

type Page struct {
	Limit  int
	Offset int
}

// GateDecision 是缓存闸门的三态：放行、未命中、拒绝。
type GateDecision int

const (
	GatePass GateDecision = iota
	GateMiss
	GateReject
)

// CouponGate 是领券前的只读缓存校验。实现必须满足：
//   - Check 不修改任何 Redis 状态；
//   - 未命中/Redis 故障时返回 GateMiss 或 error，调用方回源 DB；
//   - Warm 只写快照，库存事实仍以 MySQL 为准。
type CouponGate interface {
	Check(ctx context.Context, couponID string, now time.Time) (GateDecision, string, error)
	Warm(ctx context.Context, item *coupon.Coupon) error
	WarmRules(ctx context.Context, item *coupon.Coupon) error
	Consume(ctx context.Context, couponID string) error
	Remaining(ctx context.Context, couponID string) (int64, bool, error)
	Reset(ctx context.Context, item *coupon.Coupon) error
}

type Store interface {
	ListCoupons(context.Context, string, time.Time, Page) ([]*coupon.Coupon, error)
	ListUserCoupons(context.Context, string, string, Page) ([]*coupon.UserCoupon, error)
	UserCoupon(context.Context, string, string) (*coupon.UserCoupon, error)
	ClaimCoupon(context.Context, string, string, string, time.Time) (*coupon.UserCoupon, error)
	ClaimByIdempotency(context.Context, string, string, string) (*coupon.UserCoupon, error)
	Coupon(context.Context, string) (*coupon.Coupon, error)
	RedeemCode(context.Context, string, string, time.Time) (*coupon.UserCoupon, error)
	GenerateRedeemCodes(context.Context, coupon.GenerateRedeemCodesInput, time.Time) ([]*coupon.RedeemCode, error)
	ListRedeemCodes(context.Context, coupon.RedeemCodeFilter, Page) ([]*coupon.RedeemCode, error)
	DisableRedeemCode(context.Context, string, time.Time) error
	Lock(context.Context, string, string, string, time.Time) (*coupon.UserCoupon, error)
	MarkUsed(context.Context, string, string, string, int64, time.Time) error
	Release(context.Context, string, string) error
	// ExpireClaims persists coupon claims whose campaign is no longer active.
	// Mine already projects this state in memory; the maintenance job makes it
	// durable so status filters and reporting do not depend on a read.
	ExpireClaims(context.Context, time.Time, int) (int64, error)
	// ReleaseStaleLocked returns claims abandoned by a checkout that never
	// reached payment. A late callback can no longer mark them used.
	ReleaseStaleLocked(context.Context, time.Time, int) (int64, error)
}

type Service struct {
	store Store
	now   func() time.Time
	gate  CouponGate
	async *AsyncClaimer
}

func New(store Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// WithGate 挂上 Redis 只读闸门。gate 为 nil 时全部请求直接走 MySQL。
func (s *Service) WithGate(gate CouponGate) *Service {
	s.gate = gate
	return s
}

// WithAsyncClaimer 打开“Redis 预扣 + Stream 异步落库”模式。nil 表示保持同步事务。
func (s *Service) WithAsyncClaimer(claimer *AsyncClaimer) *Service {
	s.async = claimer
	return s
}

func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) Available(ctx context.Context, userID string, page Page) ([]*coupon.Coupon, error) {
	return s.store.ListCoupons(ctx, userID, s.now(), normalizePage(page))
}

func (s *Service) Mine(ctx context.Context, userID, status string, page Page) ([]*coupon.UserCoupon, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "" && !coupon.ValidClaimStatus(status) {
		return nil, fmt.Errorf("%w: invalid coupon status", coupon.ErrInvalidRequest)
	}
	items, err := s.store.ListUserCoupons(ctx, userID, status, normalizePage(page))
	if err != nil {
		return nil, err
	}
	now := s.now()
	for _, item := range items {
		if item.Status == coupon.ClaimUnused && item.Coupon != nil && !item.Coupon.ActiveAt(now) {
			item.Status = coupon.ClaimExpired
		}
	}
	return items, nil
}

func (s *Service) Claim(ctx context.Context, userID, couponID string) (*coupon.UserCoupon, error) {
	return s.ClaimWithIdempotency(ctx, userID, couponID, "")
}

// ClaimWithIdempotency 是领券的唯一入口。idempotencyKey 为空时退化为“非幂等”调用，
// 适合脚本或内部直连；HTTP 入口必须由网关透传 Idempotency-Key。
func (s *Service) ClaimWithIdempotency(ctx context.Context, userID, couponID, idempotencyKey string) (*coupon.UserCoupon, error) {
	userID = strings.TrimSpace(userID)
	couponID = strings.TrimSpace(couponID)
	if userID == "" || couponID == "" {
		return nil, fmt.Errorf("%w: user and coupon ids are required", coupon.ErrInvalidRequest)
	}
	key := strings.TrimSpace(idempotencyKey)
	if len(key) > 64 {
		return nil, fmt.Errorf("%w: 幂等键不能超过 64 个字符", coupon.ErrInvalidRequest)
	}
	if s.async != nil && key != "" {
		item, handled, err := s.async.Claim(ctx, userID, couponID, key)
		if handled {
			return item, err
		}
		// handled=false：Redis 未命中或不可用，回退同步事务，幂等键仍然生效。
	}
	now := s.now()
	if s.gate != nil {
		decision, reason, err := s.gate.Check(ctx, couponID, now)
		if err != nil {
			// 缓存是加速层，故障时降级回源，不阻断领券。
			log.Printf("promotion: coupon cache check %s: %v", couponID, err)
		} else if decision == GateMiss {
			if item, loadErr := s.store.Coupon(ctx, couponID); loadErr == nil && item != nil {
				if warmErr := s.gate.Warm(ctx, item); warmErr != nil {
					log.Printf("promotion: warm coupon cache %s: %v", couponID, warmErr)
				}
				decision, reason, _ = s.gate.Check(ctx, couponID, now)
			}
		}
		if decision == GateReject {
			if reason == "" {
				reason = "该券当前不可领取"
			}
			return nil, fmt.Errorf("%w: %s", coupon.ErrConflict, reason)
		}
	}
	item, err := s.store.ClaimCoupon(ctx, userID, couponID, key, now)
	if err != nil {
		return nil, err
	}
	if s.gate != nil {
		if cerr := s.gate.Consume(ctx, couponID); cerr != nil {
			log.Printf("promotion: consume coupon cache %s: %v", couponID, cerr)
		}
	}
	return item, nil
}

// Redeem consumes one seat of a batch redeem code and grants a coupon claim
// in a single store transaction. The code string is normalized because the
// generated alphabet is uppercase Crockford Base32.
func (s *Service) Redeem(ctx context.Context, userID, code string) (*coupon.UserCoupon, error) {
	userID = strings.TrimSpace(userID)
	code = coupon.NormalizeRedeemCode(code)
	if userID == "" || code == "" {
		return nil, fmt.Errorf("%w: user and redeem code are required", coupon.ErrInvalidRequest)
	}
	if len(code) < 4 || len(code) > 16 {
		return nil, fmt.Errorf("%w: 兑换码格式不正确", coupon.ErrInvalidRequest)
	}
	return s.store.RedeemCode(ctx, userID, code, s.now())
}

// GenerateRedeemCodes is the production-side entry point. The store allocates
// a sequence range and persists each code; this method only applies the
// operator-facing validation rules.
func (s *Service) GenerateRedeemCodes(ctx context.Context, input coupon.GenerateRedeemCodesInput) ([]*coupon.RedeemCode, error) {
	input.CouponID = strings.TrimSpace(input.CouponID)
	input.BatchNo = strings.TrimSpace(input.BatchNo)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	input.Note = strings.TrimSpace(input.Note)
	if input.CouponID == "" || input.CreatedBy == "" {
		return nil, fmt.Errorf("%w: coupon and operator are required", coupon.ErrInvalidRequest)
	}
	if input.Count < 1 || input.Count > 5000 {
		return nil, fmt.Errorf("%w: 发码数量需在 1 到 5000 之间", coupon.ErrInvalidRequest)
	}
	if input.MaxRedeem < 1 || input.MaxRedeem > 1000000 {
		return nil, fmt.Errorf("%w: 单码可兑换人数不合法", coupon.ErrInvalidRequest)
	}
	if len(input.BatchNo) > 64 {
		return nil, fmt.Errorf("%w: 批次号不能超过 64 个字符", coupon.ErrInvalidRequest)
	}
	if len(input.Note) > 255 {
		return nil, fmt.Errorf("%w: 备注不能超过 255 个字符", coupon.ErrInvalidRequest)
	}
	if input.StartsAt != nil && input.EndsAt != nil && !input.StartsAt.Before(*input.EndsAt) {
		return nil, fmt.Errorf("%w: 兑换开始时间必须早于结束时间", coupon.ErrInvalidRequest)
	}
	return s.store.GenerateRedeemCodes(ctx, input, s.now())
}

func (s *Service) RedeemCodes(ctx context.Context, filter coupon.RedeemCodeFilter, page Page) ([]*coupon.RedeemCode, error) {
	filter.CouponID = strings.TrimSpace(filter.CouponID)
	filter.BatchNo = strings.TrimSpace(filter.BatchNo)
	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	if filter.Status != "" && !coupon.ValidRedeemStatus(filter.Status) {
		return nil, fmt.Errorf("%w: invalid redeem code status", coupon.ErrInvalidRequest)
	}
	return s.store.ListRedeemCodes(ctx, filter, normalizePage(page))
}

// ExportRedeemCodes is deliberately bounded. A demo export should not become
// an unbounded table scan; larger exports should move to an async job.
func (s *Service) ExportRedeemCodes(ctx context.Context, filter coupon.RedeemCodeFilter) ([]*coupon.RedeemCode, error) {
	filter.CouponID = strings.TrimSpace(filter.CouponID)
	filter.BatchNo = strings.TrimSpace(filter.BatchNo)
	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	if filter.Status != "" && !coupon.ValidRedeemStatus(filter.Status) {
		return nil, fmt.Errorf("%w: invalid redeem code status", coupon.ErrInvalidRequest)
	}
	return s.store.ListRedeemCodes(ctx, filter, Page{Limit: 5000})
}

func (s *Service) DisableRedeemCode(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: redeem code id is required", coupon.ErrInvalidRequest)
	}
	return s.store.DisableRedeemCode(ctx, id, s.now())
}

// Quote applies a claimed coupon to an order amount and optionally locks it.
// The order service stores the returned snapshot; payment never needs to know
// the coupon campaign rules.
func (s *Service) Quote(ctx context.Context, userID, userCouponID, taskID string, amountFen int64, taskType string, lock bool) (*coupon.Quote, error) {
	if strings.TrimSpace(userCouponID) == "" {
		return &coupon.Quote{OriginalAmountFen: amountFen, PayableAmountFen: amountFen}, nil
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("%w: order id is required", coupon.ErrInvalidRequest)
	}
	claim, err := s.store.UserCoupon(ctx, userID, userCouponID)
	if err != nil {
		return nil, err
	}
	if claim == nil || claim.Coupon == nil {
		return nil, coupon.ErrNotFound
	}
	quote, err := coupon.QuoteFor(claim.Coupon, claim, amountFen, taskType, s.now())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", coupon.ErrInvalidRequest, err)
	}
	if lock {
		locked, err := s.store.Lock(ctx, userID, userCouponID, taskID, s.now())
		if err != nil {
			return nil, err
		}
		quote.UserCoupon = locked
	}
	return quote, nil
}

func (s *Service) MarkUsed(ctx context.Context, userCouponID, taskID, paymentOrderID string, discountFen int64) error {
	if strings.TrimSpace(userCouponID) == "" {
		return nil
	}
	return s.store.MarkUsed(ctx, userCouponID, taskID, paymentOrderID, discountFen, s.now())
}

func (s *Service) Release(ctx context.Context, userCouponID, taskID string) error {
	if strings.TrimSpace(userCouponID) == "" {
		return nil
	}
	return s.store.Release(ctx, userCouponID, taskID)
}

func normalizePage(page Page) Page {
	if page.Limit <= 0 {
		page.Limit = 20
	}
	if page.Limit > 100 {
		page.Limit = 100
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return page
}
