package svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cityflow/internal/platform/xxljob"
)

// StartJobs registers Identity-owned maintenance handlers. The runtime is nil
// when XXL-JOB is disabled.
func (s *ServiceContext) StartJobs(ctx context.Context) error {
	if s == nil || s.XXLJob == nil {
		return nil
	}
	if s.Users == nil {
		return errors.New("identity user service is unavailable")
	}
	return s.XXLJob.Start(ctx,
		xxljob.Registration{
			Name: "identity.password-reset.purge",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				before := time.Now().UTC().Add(-24 * time.Hour)
				deleted, err := s.Users.PurgePasswordResets(ctx, before, 1000, 5)
				return fmt.Sprintf("deleted=%d", deleted), err
			},
		},
	)
}
