package svc

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/app/tracking/cmd/rpc/internal/config"
	"cityflow/internal/application/repository"
	"cityflow/internal/application/service"
	"cityflow/internal/infrastructure/cache"
	"cityflow/internal/infrastructure/event"
	"cityflow/internal/infrastructure/memory"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcadapt"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config    config.Config
	Tracking  *service.TrackingService
	Positions repository.PositionRepository
	Secret    []byte
	Identity  identityclient.Identity
	Status    *authn.StatusCache
	closeFn   func() error
	pingFn    func(context.Context) error
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		secret = "CHANGE_ME_JWT_SECRET"
		log.Printf("warning: JWT secret is not set; using development secret")
	}

	var taskClient taskclient.Task
	if len(c.TaskRPC.Endpoints) > 0 {
		taskClient = taskclient.NewTask(zrpc.MustNewClient(c.TaskRPC))
	}
	var identityClient identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identityClient = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}

	// Positions and rider-centric trajectory samples live in Redis (GEO + ZSET).
	// The in-process bus fans out to WSS subscribers in this same process.
	var positions repository.PositionRepository = memory.NewPositions()
	var redisCache *cache.Redis
	closeFn := func() error { return nil }
	pingFn := func(context.Context) error { return nil }

	redisAddr := strings.TrimSpace(c.RedisAddr)
	if redisAddr == "" {
		redisAddr = strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	}
	if redisAddr != "" {
		db := c.RedisDB
		if raw := strings.TrimSpace(os.Getenv("REDIS_DB")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				db = v
			}
		}
		redis := cache.NewRedis(redisAddr, c.RedisPassword, db)
		redisCache = redis
		positions = redis
		prevClose := closeFn
		closeFn = func() error { err := prevClose(); redis.Close(); return err }
		prevPing := pingFn
		pingFn = func(ctx context.Context) error {
			if err := prevPing(ctx); err != nil {
				return err
			}
			return redis.Ping(ctx)
		}
	}

	var tracking *service.TrackingService
	if taskClient != nil {
		tracking = service.NewTrackingService(positions, event.NewPositions(), rpcadapt.Tasks{Client: taskClient}, rpcadapt.Riders{Client: taskClient})
	} else {
		tracking = service.NewTrackingService(positions, event.NewPositions(), nil, nil)
	}
	if tracks, ok := positions.(repository.TrackReader); ok {
		tracking.WithTracks(tracks)
	}
	if redisCache != nil {
		janitorCtx, janitorCancel := context.WithCancel(context.Background())
		stopJanitor := service.RunPositionJanitor(janitorCtx, redisCache, 15*time.Second, 500)
		prevClose := closeFn
		closeFn = func() error {
			janitorCancel()
			stopJanitor()
			return prevClose()
		}
	}

	statusCache := authn.NewStatusCache(5*time.Second, func(ctx context.Context, subject, token string) (bool, error) {
		if identityClient == nil {
			return false, errors.New("identity verifier unavailable")
		}
		reply, err := identityClient.Verify(rpcctx.WithBearer(ctx, token), &identityclient.VerifyRequest{})
		if err != nil {
			return false, err
		}
		return reply.GetActive() && reply.GetSubject() == subject, nil
	})

	return &ServiceContext{
		Config: c, Tracking: tracking, Positions: positions, Secret: []byte(secret),
		Identity: identityClient, Status: statusCache,
		closeFn: closeFn, pingFn: pingFn,
	}, nil
}

func (s *ServiceContext) Close() error {
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}
func (s *ServiceContext) Ping(ctx context.Context) error {
	if s.pingFn == nil {
		return nil
	}
	return s.pingFn(ctx)
}
