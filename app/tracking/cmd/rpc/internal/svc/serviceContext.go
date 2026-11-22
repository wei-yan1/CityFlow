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
	trackingservice "cityflow/app/tracking/application/tracking"
	"cityflow/app/tracking/cmd/rpc/internal/config"
	memory "cityflow/app/tracking/internal/adapter/memory"
	trackingmysql "cityflow/app/tracking/internal/adapter/mysql"
	trackingredis "cityflow/app/tracking/internal/adapter/redis"
	"cityflow/app/tracking/repository"
	"cityflow/internal/integration/rpcadapt"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/cache"
	"cityflow/internal/platform/event"
	"cityflow/internal/platform/mq"
	platformmysql "cityflow/internal/platform/mysql"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config config.Config

	Tracking      *trackingservice.TrackingService
	Positions     repository.PositionRepository
	TrackArchive  repository.TrackArchive
	TrackQueue    repository.TrackQueue
	TrackArchiver *trackingservice.TrackArchiver
	Consumer      *mq.Consumer
	Publisher     *mq.Publisher

	Secret   []byte
	Identity identityclient.Identity
	Status   *authn.StatusCache
	closeFn  func() error
	pingFn   func(context.Context) error
}

// taskViews adapts Task's RPC snapshot to Tracking's local projection.
type taskViews struct{ tasks rpcadapt.Tasks }

func (a taskViews) Get(ctx context.Context, id string) (*trackingservice.TaskView, error) {
	item, err := a.tasks.Snapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &trackingservice.TaskView{
		ID: item.ID, Status: item.Status, CreatedBy: item.CreatedBy, WorkerID: item.WorkerID,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	for _, event := range item.Events {
		out.Events = append(out.Events, trackingservice.TaskEvent{To: event.To, ActorID: event.ActorID, CreatedAt: event.CreatedAt})
	}
	return out, nil
}

// riderViews adapts Task's rider profile to the status-only view Tracking uses.
type riderViews struct{ riders rpcadapt.Riders }

func (a riderViews) RiderForUser(ctx context.Context, userID string) (*trackingservice.RiderView, error) {
	profile, err := a.riders.RiderForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &trackingservice.RiderView{Status: profile.Status}, nil
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		log.Fatalf("JWT secret is not configured: set CITYFLOW_JWT_SECRET in .env")
	}

	var taskClient taskclient.Task
	if len(c.TaskRPC.Endpoints) > 0 {
		taskClient = taskclient.NewTask(zrpc.MustNewClient(c.TaskRPC))
	}
	var identityClient identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identityClient = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}

	// Positions and rider-centric trajectory samples are written to Redis
	// first (GEO + pending ZSET); the MQ consumer archives them to MySQL.
	// The in-process bus fans out to WSS subscribers in this same process.
	var positions repository.PositionRepository = memory.NewPositions()
	var trackQueue repository.TrackQueue
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
		store := trackingredis.New(redis)
		positions = store
		trackQueue = store
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

	var trackArchive repository.TrackArchive
	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("TRACKING_DATABASE_DSN"))
	}
	if dsn != "" {
		openCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		db, err := platformmysql.Open(openCtx, dsn)
		if err != nil {
			return nil, err
		}
		if err := platformmysql.MigrateService(openCtx, db, "tracking"); err != nil {
			_ = db.Close()
			return nil, err
		}
		store := trackingmysql.New(db)
		trackArchive = store
		prevClose := closeFn
		closeFn = func() error {
			err := prevClose()
			if closeErr := db.Close(); err == nil {
				err = closeErr
			}
			return err
		}
		prevPing := pingFn
		pingFn = func(ctx context.Context) error {
			if err := prevPing(ctx); err != nil {
				return err
			}
			return db.Ping(ctx)
		}
	} else {
		log.Printf("warning: TRACKING_DATABASE_DSN is empty; trajectory MySQL archive is disabled")
	}

	var tracking *trackingservice.TrackingService
	if taskClient != nil {
		tracking = trackingservice.NewTrackingService(positions, event.NewPositions(), taskViews{tasks: rpcadapt.Tasks{Client: taskClient}}, riderViews{riders: rpcadapt.Riders{Client: taskClient}})
	} else {
		tracking = trackingservice.NewTrackingService(positions, event.NewPositions(), nil, nil)
	}
	pendingTracks, _ := positions.(repository.TrackReader)
	switch {
	case trackArchive != nil && pendingTracks != nil:
		tracking.WithTracks(trackingservice.NewMergedTrackReader(trackArchive, pendingTracks))
	case trackArchive != nil:
		tracking.WithTracks(trackArchive)
	case pendingTracks != nil:
		tracking.WithTracks(pendingTracks)
	}
	if janitor, ok := positions.(repository.PositionJanitor); ok {
		janitorCtx, janitorCancel := context.WithCancel(context.Background())
		stopJanitor := trackingservice.RunPositionJanitor(janitorCtx, janitor, 15*time.Second, 500)
		prevClose := closeFn
		closeFn = func() error {
			janitorCancel()
			stopJanitor()
			return prevClose()
		}
	}

	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	var consumer *mq.Consumer
	var publisher *mq.Publisher
	var archiver *trackingservice.TrackArchiver
	if mqURL != "" && trackQueue != nil && trackArchive != nil {
		publisher = mq.NewPublisher(mqURL)
		consumer = mq.NewConsumer(mqURL, "tracking.track-archive", trackingservice.TrackArchiveTopic)
		archiver = trackingservice.NewTrackArchiver(trackQueue, publisher)
		prevClose := closeFn
		closeFn = func() error {
			err := prevClose()
			if closeErr := publisher.Close(); err == nil {
				err = closeErr
			}
			return err
		}
	} else {
		log.Printf("warning: trajectory MQ archive is disabled (rabbitmq=%t, redis=%t, mysql=%t)", mqURL != "", trackQueue != nil, trackArchive != nil)
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
		Config: c, Tracking: tracking, Positions: positions,
		TrackArchive: trackArchive, TrackQueue: trackQueue, TrackArchiver: archiver,
		Consumer: consumer, Publisher: publisher, Secret: []byte(secret),
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
