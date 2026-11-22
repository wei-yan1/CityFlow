package svc

import (
	"context"
	"errors"
	"fmt"

	"cityflow/internal/platform/xxljob"
)

// StartJobs registers the notification retention handler. The runtime is nil
// when XXL-JOB is disabled.
func (s *ServiceContext) StartJobs(ctx context.Context) error {
	if s == nil || s.XXLJob == nil {
		return nil
	}
	if s.Service == nil {
		return errors.New("notification service is unavailable")
	}
	return s.XXLJob.Start(ctx,
		xxljob.Registration{
			Name: "notification.retention",
			Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
				deleted, err := s.Service.PurgeRetention(ctx, s.ReadRetention, s.UnreadRetention, 1000, 5)
				return fmt.Sprintf("deleted=%d read_retention=%s unread_retention=%s", deleted, s.ReadRetention, s.UnreadRetention), err
			},
		},
	)
}
