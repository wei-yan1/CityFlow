package svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cityflow/internal/platform/xxljob"
)

// StartJobs registers Discovery-owned maintenance handlers. The runtime is nil
// when XXL-JOB is disabled.
func (s *ServiceContext) StartJobs(ctx context.Context) error {
	if s == nil || s.XXLJob == nil {
		return nil
	}
	if s.Trusted == nil {
		return errors.New("trusted place service is unavailable")
	}
	return s.XXLJob.Start(ctx,
		xxljob.Registration{
			Name: "discovery.trusted-places.purge",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				deleted, err := s.Trusted.PurgeExpired(ctx, time.Now().UTC(), 1000, 5)
				return fmt.Sprintf("deleted=%d", deleted), err
			},
		},
	)
}
