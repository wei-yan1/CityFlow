package svc

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"cityflow/app/identity/cmd/rpc/internal/config"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/application/repository"
	"cityflow/internal/application/service"
	"cityflow/internal/infrastructure/cache"
	"cityflow/internal/infrastructure/memory"
	"cityflow/internal/infrastructure/mq"
	mysqlstore "cityflow/internal/infrastructure/mysql"

	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config        config.Config
	Auth          *service.AuthService
	Accounts      repository.AuthRepository
	Refresh       service.RefreshStore
	Secret        []byte
	Users         *service.UserService
	Riders        *service.RiderRegistrationService
	Task          taskclient.Task
	EventsEnabled bool
	Relay         *service.OutboxRelay
	publisher     *mq.Publisher
	closeFn       func() error
	pingFn        func(context.Context) error
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		secret = "cityflow-development-secret-change-me"
		log.Printf("warning: JWT secret is not set; using development secret")
	}

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("IDENTITY_DATABASE_DSN"))
	}

	var (
		accounts    repository.AuthRepository
		outboxStore repository.OutboxStore
		closeFn     = func() error { return nil }
		pingFn      = func(context.Context) error { return nil }
	)
	if dsn == "" {
		store, err := memory.NewStore()
		if err != nil {
			return nil, err
		}
		accounts = store
		closeFn = store.Close
		pingFn = func(context.Context) error { return nil }
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		store, err := mysqlstore.Open(ctx, dsn)
		if err != nil {
			cancel()
			return nil, err
		}
		if err := store.MigrateService(ctx, "identity"); err != nil {
			cancel()
			store.Close()
			return nil, err
		}
		if c.SeedDemo && strings.TrimSpace(os.Getenv("CITYFLOW_SEED_DEMO")) != "false" {
			if err := store.SeedDemoIdentity(ctx); err != nil {
				cancel()
				store.Close()
				return nil, err
			}
		}
		cancel()
		accounts = store
		outboxStore = mysqlstore.IdentityOutboxStore{Store: store}
		closeFn = func() error { store.Close(); return nil }
		pingFn = store.Ping
	}

	// Identity owns accounts only. Merchant and rider profile ownership moves
	// out of this service; the methods that used them are not exposed by the
	// Identity RPC.
	authService := service.NewAuthService(accounts, nil, nil, []byte(secret))

	var users *service.UserService
	if managed, ok := accounts.(repository.UserRepository); ok {
		users = service.NewUserService(accounts, managed, nil)
		// Local/demo builds expose the reset token so the browser reset flow can
		// be completed without an email provider. Production can disable it with
		// CITYFLOW_EXPOSE_RESET_TOKEN=false, matching the previous HTTP behavior.
		users = users.WithResetTokenExposure(strings.TrimSpace(os.Getenv("CITYFLOW_EXPOSE_RESET_TOKEN")) != "false")
	}
	var registrations *service.RiderRegistrationService
	if regRepo, ok := accounts.(repository.RiderRegistrationRepository); ok {
		registrations = service.NewRiderRegistrationService(regRepo)
	}
	var taskClient taskclient.Task
	if len(c.TaskRpc.Endpoints) > 0 || c.TaskRpc.Target != "" {
		taskClient = taskclient.NewTask(zrpc.MustNewClient(c.TaskRpc))
	}

	var refresh service.RefreshStore
	var redis *cache.Redis
	redisAddr := strings.TrimSpace(c.RedisAddr)
	if redisAddr == "" {
		redisAddr = strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	}
	if redisAddr != "" {
		redisPassword := c.RedisPassword
		if redisPassword == "" {
			redisPassword = os.Getenv("REDIS_PASSWORD")
		}
		redisDB := c.RedisDB
		if raw := strings.TrimSpace(os.Getenv("REDIS_DB")); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				redisDB = value
			}
		}
		redis = cache.NewRedis(redisAddr, redisPassword, redisDB)
		refresh = redis
		previousClose := closeFn
		closeFn = func() error {
			err := previousClose()
			if redisErr := redis.Close(); err == nil {
				err = redisErr
			}
			return err
		}
		previousPing := pingFn
		pingFn = func(ctx context.Context) error {
			if err := previousPing(ctx); err != nil {
				return err
			}
			return redis.Ping(ctx)
		}
	}

	var publisher *mq.Publisher
	var relay *service.OutboxRelay
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL != "" && outboxStore != nil {
		publisher = mq.NewPublisher(mqURL)
		probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
		probeErr := publisher.Probe(probeCtx)
		cancelProbe()
		if probeErr != nil {
			log.Printf("warning: RabbitMQ unavailable (%v); rider approval falls back to synchronous provisioning", probeErr)
			_ = publisher.Close()
			publisher = nil
		} else {
			relay = service.NewOutboxRelay(outboxStore, publisher, 2*time.Second, 50)
			previousClose := closeFn
			closeFn = func() error {
				err := previousClose()
				if pubErr := publisher.Close(); err == nil {
					err = pubErr
				}
				return err
			}
		}
	} else if outboxStore != nil {
		log.Printf("warning: RABBITMQ_URL is empty; rider approval outbox will stay pending")
	}

	return &ServiceContext{
		Config:        c,
		Auth:          authService,
		Accounts:      accounts,
		Refresh:       refresh,
		Secret:        []byte(secret),
		Users:         users,
		Riders:        registrations,
		Task:          taskClient,
		EventsEnabled: relay != nil,
		Relay:         relay,
		publisher:     publisher,
		closeFn:       closeFn,
		pingFn:        pingFn,
	}, nil
}

func (s *ServiceContext) Close() error {
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

func (s *ServiceContext) Ping(ctx context.Context) error {
	if s.pingFn == nil {
		return nil
	}
	if err := s.pingFn(ctx); err != nil {
		return errors.Join(errors.New("identity database unavailable"), err)
	}
	return nil
}
