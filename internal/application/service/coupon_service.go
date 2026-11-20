package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/coupon"
	"cityflow/internal/domain/task"
)

type CouponService struct {
	repo  repository.CouponRepository
	tasks taskReader
	now   func() time.Time
}

func NewCouponService(repo repository.CouponRepository, tasks taskReader) *CouponService {
	return &CouponService{repo: repo, tasks: tasks, now: func() time.Time { return time.Now().UTC() }}
}

func (s *CouponService) WithClock(now func() time.Time) *CouponService {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *CouponService) Available(ctx context.Context, userID string, page repository.Page) ([]*coupon.Coupon, error) {
	return s.repo.ListCoupons(ctx, userID, s.now(), page)
}

func (s *CouponService) Mine(ctx context.Context, userID, status string, page repository.Page) ([]*coupon.UserCoupon, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "" && !coupon.ValidClaimStatus(status) {
		return nil, fmt.Errorf("%w: invalid coupon status", domain.ErrInvalidRequest)
	}
	items, err := s.repo.ListUserCoupons(ctx, userID, status, page)
	if err != nil {
		return nil, err
	}
	now := s.now()
	for _, item := range items {
		if item.Status == coupon.ClaimUnused && item.Coupon != nil && !item.Coupon.ActiveAt(now) {
			// The persisted row remains UNUSED until the expiry worker touches it;
			// the read projection tells the UI the truth without a write in a GET.
			item.Status = coupon.ClaimExpired
		}
	}
	return items, nil
}

func (s *CouponService) Claim(ctx context.Context, userID, couponID string) (*coupon.UserCoupon, error) {
	if strings.TrimSpace(couponID) == "" {
		return nil, fmt.Errorf("%w: coupon id is required", domain.ErrInvalidRequest)
	}
	return s.repo.ClaimCoupon(ctx, userID, couponID, s.now())
}

// QuotesForTask returns only claims that can actually be applied to this task.
// Ineligible coupons are omitted instead of shown as disabled noise in the
// payment dialog.
func (s *CouponService) QuotesForTask(ctx context.Context, userID, taskID string) ([]*coupon.Quote, error) {
	item, err := s.taskForUser(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	claims, err := s.repo.ListUserCoupons(ctx, userID, coupon.ClaimUnused, repository.Page{Limit: 200})
	if err != nil {
		return nil, err
	}
	now := s.now()
	result := make([]*coupon.Quote, 0, len(claims))
	for _, claim := range claims {
		campaign := claim.Coupon
		if campaign == nil {
			loaded, loadErr := s.repo.UserCoupon(ctx, userID, claim.ID)
			if loadErr != nil {
				continue
			}
			claim, campaign = loaded, loaded.Coupon
		}
		quote, quoteErr := coupon.QuoteFor(campaign, claim, *item.QuotedAmountFen, item.Type, now)
		if quoteErr != nil {
			continue
		}
		result = append(result, quote)
	}
	return result, nil
}

func (s *CouponService) QuoteForTask(ctx context.Context, userID, userCouponID, taskID string) (*coupon.Quote, error) {
	item, err := s.taskForUser(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(userCouponID) == "" {
		return nil, fmt.Errorf("%w: coupon id is required", domain.ErrInvalidRequest)
	}
	claim, err := s.repo.UserCoupon(ctx, userID, userCouponID)
	if err != nil {
		return nil, err
	}
	return coupon.QuoteFor(claim.Coupon, claim, *item.QuotedAmountFen, item.Type, s.now())
}

// LockForTask is called only when a new payment order is created. The payment
// service then stores the quote snapshot on the order.
func (s *CouponService) LockForTask(ctx context.Context, userID, userCouponID, taskID string) (*coupon.Quote, error) {
	quote, err := s.QuoteForTask(ctx, userID, userCouponID, taskID)
	if err != nil {
		return nil, err
	}
	claim, err := s.repo.Lock(ctx, userID, userCouponID, taskID, s.now())
	if err != nil {
		return nil, err
	}
	quote.UserCoupon = claim
	return quote, nil
}

func (s *CouponService) MarkUsed(ctx context.Context, userCouponID, taskID, paymentOrderID string, discountFen int64) error {
	if strings.TrimSpace(userCouponID) == "" {
		return nil
	}
	return s.repo.MarkUsed(ctx, userCouponID, taskID, paymentOrderID, discountFen, s.now())
}

func (s *CouponService) Release(ctx context.Context, userCouponID, taskID string) {
	if strings.TrimSpace(userCouponID) == "" {
		return
	}
	// Coupon release is best-effort: the payment state is already authoritative,
	// and a stale LOCKED claim can be repaired by a later reconciliation.
	_ = s.repo.Release(ctx, userCouponID, taskID)
}

func (s *CouponService) taskForUser(ctx context.Context, userID, taskID string) (*task.Task, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("%w: task id is required", domain.ErrInvalidRequest)
	}
	item, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if item.CreatedBy != userID {
		return nil, domain.ErrForbidden
	}
	if item.Status != task.StatusOpen {
		return nil, fmt.Errorf("%w: 只有待接单的任务可以使用优惠券", domain.ErrConflict)
	}
	if item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
		return nil, fmt.Errorf("%w: 任务未设置报酬金额", domain.ErrInvalidRequest)
	}
	return item, nil
}

// Compile-time check: the service satisfies the same small gate the payment
// service depends on. Keeping the interface local avoids a package cycle.
var _ interface {
	LockForTask(context.Context, string, string, string) (*coupon.Quote, error)
	MarkUsed(context.Context, string, string, string, int64) error
	Release(context.Context, string, string)
} = (*CouponService)(nil)
