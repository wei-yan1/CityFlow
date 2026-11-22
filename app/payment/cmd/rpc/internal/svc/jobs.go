package svc

import (
	"context"
	"errors"
	"fmt"

	"cityflow/internal/platform/xxljob"
)

// StartJobs registers the payment maintenance handlers. The runtime is nil
// when XXL-JOB is disabled, so all registrations become no-ops.
func (s *ServiceContext) StartJobs(ctx context.Context) error {
	if s == nil || s.XXLJob == nil {
		return nil
	}
	if s.Payments == nil {
		return errors.New("payment service is unavailable")
	}
	return s.XXLJob.Start(ctx,
		xxljob.Registration{
			Name: "payment.reconcile",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				payments, refunds := s.Payments.ReconcileJob(ctx)
				return fmt.Sprintf("reconciled payments=%d refunds=%d", payments, refunds), nil
			},
		},
		xxljob.Registration{
			Name: "payment.inbox.purge",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				deleted, err := s.Payments.PurgeInboxJob(ctx)
				return fmt.Sprintf("deleted=%d", deleted), err
			},
		},
		xxljob.Registration{
			Name: "payment.notify.purge",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				deleted, err := s.Payments.PurgeNotifyLogsJob(ctx, s.NotifyRetention, 1000, 5)
				return fmt.Sprintf("deleted=%d retention=%s", deleted, s.NotifyRetention), err
			},
		},
	)
}
