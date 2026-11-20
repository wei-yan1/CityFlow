package repository

import (
	"context"
	"time"

	"cityflow/internal/domain/coupon"
)

// CouponRepository owns campaign inventory and one claim per (coupon,user,slot).
// Claim/Lock are transactional in the persistent implementation so two
// concurrent requests cannot exceed inventory or lock one claim twice.
type CouponRepository interface {
	ListCoupons(context.Context, string, time.Time, Page) ([]*coupon.Coupon, error)
	ListUserCoupons(context.Context, string, string, Page) ([]*coupon.UserCoupon, error)
	UserCoupon(context.Context, string, string) (*coupon.UserCoupon, error)
	ClaimCoupon(context.Context, string, string, time.Time) (*coupon.UserCoupon, error)
	Lock(context.Context, string, string, string, time.Time) (*coupon.UserCoupon, error)
	MarkUsed(context.Context, string, string, string, int64, time.Time) error
	Release(context.Context, string, string) error
}
