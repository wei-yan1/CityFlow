// Package coupon defines platform delivery coupons: campaign rules, a user's
// claim, and the discount calculation used before a payment order is created.
// A coupon never moves money by itself; it only reduces the amount charged and
// the platform continues to settle the rider against the original task quote.
package coupon

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid promotion request")
	ErrNotFound       = errors.New("promotion not found")
	ErrConflict       = errors.New("promotion conflict")
	ErrUnavailable    = errors.New("promotion unavailable")
	// ErrPending 表示预扣已经成功、落库还在队列里；调用方必须用同一个幂等键重试。
	ErrPending = errors.New("promotion claim pending")
)

const (
	TypeAmount  = "AMOUNT"
	TypePercent = "PERCENT"
)

const (
	StatusDraft  = "DRAFT"
	StatusActive = "ACTIVE"
	StatusPaused = "PAUSED"
	StatusEnded  = "ENDED"
)

const (
	ClaimUnused  = "UNUSED"
	ClaimLocked  = "LOCKED"
	ClaimUsed    = "USED"
	ClaimExpired = "EXPIRED"
)

const (
	RedeemStatusActive   = "ACTIVE"
	RedeemStatusDisabled = "DISABLED"
)

// Coupon is the campaign/template. ClaimedCount is maintained atomically when
// a user claims it; Remaining is a read-only projection for the UI.
type Coupon struct {
	StartsAt            *time.Time `json:"starts_at,omitempty"`
	EndsAt              *time.Time `json:"ends_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	ID                  string     `json:"id"`
	Code                string     `json:"code"`
	Title               string     `json:"title"`
	Subtitle            string     `json:"subtitle,omitempty"`
	Description         string     `json:"description,omitempty"`
	Type                string     `json:"type"`
	ApplicableTaskTypes []string   `json:"applicable_task_types,omitempty"`
	ValueFen            int64      `json:"value_fen,omitempty"`
	DiscountPercent     int        `json:"discount_percent,omitempty"`
	MaxDiscountFen      int64      `json:"max_discount_fen,omitempty"`
	MinSpendFen         int64      `json:"min_spend_fen,omitempty"`
	TotalQuantity       int        `json:"total_quantity"`
	ClaimedCount        int        `json:"claimed_count"`
	PerUserLimit        int        `json:"per_user_limit"`
	Status              string     `json:"status"`
	Remaining           int        `json:"remaining"`
	UserClaimCount      int        `json:"user_claim_count,omitempty"`
	UserCouponID        string     `json:"user_coupon_id,omitempty"`
	UserCouponStatus    string     `json:"user_coupon_status,omitempty"`
}

// UserCoupon is one claimed instance. Coupon is attached for read APIs, while
// the claim row is the source of truth for whether it can still be used.
type UserCoupon struct {
	Coupon       *Coupon    `json:"coupon,omitempty"`
	ClaimedAt    time.Time  `json:"claimed_at"`
	UsedAt       *time.Time `json:"used_at,omitempty"`
	ID           string     `json:"id"`
	CouponID     string     `json:"coupon_id"`
	UserID       string     `json:"user_id"`
	Status       string     `json:"status"`
	TaskID       string     `json:"task_id,omitempty"`
	PaymentOrder string     `json:"payment_order_id,omitempty"`
	ClaimNo      int        `json:"claim_no"`
	DiscountFen  int64      `json:"discount_fen,omitempty"`
}

// RedeemCode is one shared batch code. It is not a coupon instance: many users
// may redeem it up to MaxRedeem, and each redemption creates a separate claim.
type RedeemCode struct {
	StartsAt  *time.Time `json:"starts_at,omitempty"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ID        string     `json:"id"`
	CouponID  string     `json:"coupon_id"`
	BatchNo   string     `json:"batch_no"`
	Code      string     `json:"code"`
	Seq       uint64     `json:"seq"`
	Status    string     `json:"status"`
	CreatedBy string     `json:"created_by"`
	Note      string     `json:"note,omitempty"`
	MaxRedeem int        `json:"max_redeem"`
	UsedCount int        `json:"used_count"`
}

func (r *RedeemCode) ActiveAt(now time.Time) bool {
	if r.Status != RedeemStatusActive {
		return false
	}
	if r.StartsAt != nil && now.Before(*r.StartsAt) {
		return false
	}
	if r.EndsAt != nil && !now.Before(*r.EndsAt) {
		return false
	}
	return true
}

func ValidRedeemStatus(value string) bool {
	return value == RedeemStatusActive || value == RedeemStatusDisabled
}

// GenerateRedeemCodesInput is the admin-side batch request. Count is the
// number of code rows; MaxRedeem is the shared seat count on each row.
type GenerateRedeemCodesInput struct {
	StartsAt  *time.Time
	EndsAt    *time.Time
	CouponID  string
	BatchNo   string
	CreatedBy string
	Note      string
	Count     int
	MaxRedeem int
}

// RedeemCodeFilter is used by the admin list and CSV export.
type RedeemCodeFilter struct {
	CouponID string
	BatchNo  string
	Status   string
}

// Quote is the server-side answer to "what would this coupon do for this task?"
// It is also the snapshot copied into a payment order.
type Quote struct {
	Coupon            *Coupon     `json:"coupon"`
	UserCoupon        *UserCoupon `json:"user_coupon"`
	OriginalAmountFen int64       `json:"original_amount_fen"`
	DiscountFen       int64       `json:"discount_fen"`
	PayableAmountFen  int64       `json:"payable_amount_fen"`
}

func ValidClaimStatus(value string) bool {
	switch value {
	case ClaimUnused, ClaimLocked, ClaimUsed, ClaimExpired:
		return true
	default:
		return false
	}
}

func (c *Coupon) Applicable(taskType string) bool {
	if len(c.ApplicableTaskTypes) == 0 {
		return true
	}
	taskType = strings.ToUpper(strings.TrimSpace(taskType))
	for _, candidate := range c.ApplicableTaskTypes {
		if strings.EqualFold(strings.TrimSpace(candidate), taskType) {
			return true
		}
	}
	return false
}

func (c *Coupon) ActiveAt(now time.Time) bool {
	if c.Status != StatusActive {
		return false
	}
	if c.StartsAt != nil && now.Before(*c.StartsAt) {
		return false
	}
	if c.EndsAt != nil && !now.Before(*c.EndsAt) {
		return false
	}
	return true
}

// DiscountFor returns the actual discount in fen. It never equals the whole
// order: payment providers and the payment order model require a positive
// charge, so the payer always has at least one fen left to pay.
func (c *Coupon) DiscountFor(amountFen int64) (int64, error) {
	if amountFen <= 0 {
		return 0, errors.New("order amount must be positive")
	}
	if c.MinSpendFen > 0 && amountFen < c.MinSpendFen {
		return 0, fmt.Errorf("任务金额需满 %s 才能使用该券", yuan(c.MinSpendFen))
	}
	var discount int64
	switch c.Type {
	case TypeAmount:
		discount = c.ValueFen
	case TypePercent:
		discount = amountFen * int64(c.DiscountPercent) / 100
	default:
		return 0, errors.New("unsupported coupon type")
	}
	if c.MaxDiscountFen > 0 && discount > c.MaxDiscountFen {
		discount = c.MaxDiscountFen
	}
	if discount <= 0 {
		return 0, errors.New("该券不适用于当前任务")
	}
	if discount >= amountFen {
		discount = amountFen - 1
	}
	if discount <= 0 {
		return 0, errors.New("任务金额过低，无法使用该券")
	}
	return discount, nil
}

// Quote applies a campaign and claim to a concrete order amount/type.
func QuoteFor(c *Coupon, claim *UserCoupon, amountFen int64, taskType string, now time.Time) (*Quote, error) {
	if c == nil {
		return nil, errors.New("coupon not found")
	}
	if claim != nil {
		if claim.Status != ClaimUnused && claim.Status != ClaimLocked {
			return nil, errors.New("该券已被使用或已失效")
		}
		if claim.CouponID != c.ID {
			return nil, errors.New("coupon claim does not match the campaign")
		}
	}
	if !c.ActiveAt(now) {
		return nil, errors.New("该券当前不可领取或使用")
	}
	if c.Remaining <= 0 && claim == nil {
		return nil, errors.New("该券已领完")
	}
	if !c.Applicable(taskType) {
		return nil, errors.New("该券不适用于此类任务")
	}
	discount, err := c.DiscountFor(amountFen)
	if err != nil {
		return nil, err
	}
	return &Quote{
		Coupon: c, UserCoupon: claim, OriginalAmountFen: amountFen,
		DiscountFen: discount, PayableAmountFen: amountFen - discount,
	}, nil
}

func yuan(fen int64) string {
	return fmt.Sprintf("%.2f 元", float64(fen)/100)
}

