package svc

import (
	"context"
	"errors"
	"fmt"

	"cityflow/internal/platform/xxljob"
)

// StartJobs registers the promotion claim maintenance handler. The runtime is
// nil when XXL-JOB is disabled, so this is a no-op in local mode.
func (s *ServiceContext) StartJobs(ctx context.Context) error {
	if s == nil || s.XXLJob == nil {
		return nil
	}
	if s.Service == nil {
		return errors.New("promotion service is unavailable")
	}
	return s.XXLJob.Start(ctx,
		xxljob.Registration{
			Name: "promotion.claim.expire",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				released, expired, err := s.Service.ExpireClaims(ctx)
				return fmt.Sprintf("released=%d expired=%d", released, expired), err
			},
		},
		xxljob.Registration{
			Name: "promotion.coupon.cache",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				warmed, err := s.Service.RefreshCouponCache(ctx)
				return fmt.Sprintf("warmed=%d", warmed), err
			},
		},
		xxljob.Registration{
			Name: "promotion.coupon.reconcile",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				checked, repaired, err := s.Service.ReconcileCouponCache(ctx)
				return fmt.Sprintf("checked=%d repaired=%d", checked, repaired), err
			},
		},
	)
}
