package svc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/app/gateway/cmd/api/internal/config"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	paymentclient "cityflow/app/payment/cmd/rpc/paymentclient"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	trackingclient "cityflow/app/tracking/cmd/rpc/trackingclient"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServiceContext struct {
	Config    config.Config
	Identity  identityclient.Identity
	Task      taskclient.Task
	Discovery discoveryclient.Discovery
	Payment   paymentclient.Payment
	Tracking  trackingclient.Tracking
	Reaction  reactionclient.Reaction
	Secret    []byte
	Status    *authn.StatusCache
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	if len(c.IdentityRPC.Endpoints) == 0 {
		return nil, errors.New("IdentityRPC.Endpoints is required")
	}
	if len(c.TaskRPC.Endpoints) == 0 {
		return nil, errors.New("TaskRPC.Endpoints is required")
	}
	if len(c.DiscoveryRPC.Endpoints) == 0 {
		return nil, errors.New("DiscoveryRPC.Endpoints is required")
	}
	if len(c.PaymentRPC.Endpoints) == 0 {
		return nil, errors.New("PaymentRPC.Endpoints is required")
	}
	if len(c.TrackingRPC.Endpoints) == 0 {
		return nil, errors.New("TrackingRPC.Endpoints is required")
	}
	var reactionRPC zrpc.Client
	if len(c.ReactionRPC.Endpoints) > 0 {
		var err error
		reactionRPC, err = zrpc.NewClient(c.ReactionRPC)
		if err != nil {
			return nil, fmt.Errorf("reaction rpc client: %w", err)
		}
	}
	identityRPC, err := zrpc.NewClient(c.IdentityRPC)
	if err != nil {
		return nil, fmt.Errorf("identity rpc client: %w", err)
	}
	taskRPC, err := zrpc.NewClient(c.TaskRPC)
	if err != nil {
		return nil, fmt.Errorf("task rpc client: %w", err)
	}

	discoveryRPC, err := zrpc.NewClient(c.DiscoveryRPC)
	if err != nil {
		return nil, fmt.Errorf("discovery rpc client: %w", err)
	}
	paymentRPC, err := zrpc.NewClient(c.PaymentRPC)
	if err != nil {
		return nil, fmt.Errorf("payment rpc client: %w", err)
	}
	trackingRPC, err := zrpc.NewClient(c.TrackingRPC)
	if err != nil {
		return nil, fmt.Errorf("tracking rpc client: %w", err)
	}

	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		secret = "CHANGE_ME_JWT_SECRET"
		log.Printf("warning: JWT secret is not set; using development secret")
	}

	statusTTL := time.Duration(c.AuthStatusCacheTTLSeconds) * time.Second
	if raw := strings.TrimSpace(os.Getenv("AUTH_STATUS_CACHE_TTL_SECONDS")); raw != "" {
		if value, parseErr := strconv.Atoi(raw); parseErr == nil {
			statusTTL = time.Duration(value) * time.Second
		}
	}
	if statusTTL <= 0 {
		statusTTL = 5 * time.Second
	}
	if statusTTL > 300*time.Second {
		return nil, errors.New("AuthStatusCacheTTLSeconds must be at most 300")
	}

	identity := identityclient.NewIdentity(identityRPC)
	discovery := discoveryclient.NewDiscovery(discoveryRPC)
	payment := paymentclient.NewPayment(paymentRPC)
	tracking := trackingclient.NewTracking(trackingRPC)
	statusCache := authn.NewStatusCache(statusTTL, func(ctx context.Context, subject, token string) (bool, error) {
		reply, err := identity.Verify(rpcctx.WithBearer(ctx, token), &identityclient.VerifyRequest{})
		if err != nil {
			if status.Code(err) == codes.Unauthenticated {
				return false, nil
			}
			return false, err
		}
		return reply.GetActive() && reply.GetSubject() == subject, nil
	})

	return &ServiceContext{
		Config:    c,
		Identity:  identity,
		Task:      taskclient.NewTask(taskRPC),
		Discovery: discovery,
		Payment:   payment,
		Tracking:  tracking,
		Reaction:  reactionClientOrNil(reactionRPC),
		Secret:    []byte(secret),
		Status:    statusCache,
	}, nil
}

func reactionClientOrNil(client zrpc.Client) reactionclient.Reaction {
	if client == nil {
		return nil
	}
	return reactionclient.NewReaction(client)
}

func (s *ServiceContext) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := s.Identity.Health(ctx, &identityclient.Empty{}); err != nil {
		return fmt.Errorf("identity rpc: %w", err)
	}
	if _, err := s.Task.Health(ctx, &taskclient.Empty{}); err != nil {
		return fmt.Errorf("task rpc: %w", err)
	}
	if _, err := s.Discovery.Health(ctx, &discoveryclient.Empty{}); err != nil {
		return fmt.Errorf("discovery rpc: %w", err)
	}
	if _, err := s.Payment.Health(ctx, &paymentclient.Empty{}); err != nil {
		return fmt.Errorf("payment rpc: %w", err)
	}
	if _, err := s.Tracking.Health(ctx, &trackingclient.Empty{}); err != nil {
		return fmt.Errorf("tracking rpc: %w", err)
	}
	return nil
}

// WaitReady is the startup gate. Gateway refuses to bind its public port until
// every domain service answers its health RPC.
func (s *ServiceContext) WaitReady(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		last = s.Ready(attemptCtx)
		cancel()
		if last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
