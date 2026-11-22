package svc

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	authservice "cityflow/app/identity/application/auth"
	riderregservice "cityflow/app/identity/application/riderreg"
	"cityflow/app/identity/application/user"
	"cityflow/app/identity/cmd/rpc/internal/config"
	identitymysql "cityflow/app/identity/internal/adapter/mysql"
	identityredis "cityflow/app/identity/internal/adapter/redis"
	"cityflow/app/identity/repository"
	"cityflow/internal/common/outbox"
	"cityflow/internal/platform/cache"
	"cityflow/internal/platform/mq"
	platformmysql "cityflow/internal/platform/mysql"
	outboxservice "cityflow/internal/platform/outboxservice"
	"cityflow/internal/platform/xxljob"
)

type ServiceContext struct {
	Config    config.Config
	Auth      *authservice.AuthService
	Refresh   authservice.RefreshStore
	Secret    []byte
	Users     *userservice.UserService
	Riders    *riderregservice.RiderRegistrationService
	Relay     *outboxservice.OutboxRelay
	XXLJob    *xxljob.Runtime
	accounts  repository.AuthRepository
	publisher *mq.Publisher
	closeFn   func() error
	pingFn    func(context.Context) error
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		log.Fatalf("JWT secret is not configured: set CITYFLOW_JWT_SECRET in .env")
	}

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("IDENTITY_DATABASE_DSN"))
	}

	var (
		accounts    repository.AuthRepository
		outboxStore outbox.Store
		closeFn     = func() error { return nil }
		pingFn      = func(context.Context) error { return nil }
	)
	if dsn == "" {
		return nil, errors.New("IDENTITY_DATABASE_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := platformmysql.MigrateService(ctx, db, "identity"); err != nil {
		cancel()
		_ = db.Close()
		return nil, err
	}
	store := identitymysql.New(db)
	seedDemo := c.SeedDemo
	if raw := strings.TrimSpace(os.Getenv("CITYFLOW_SEED_DEMO")); raw != "" {
		seedDemo = raw == "true"
	}
	if seedDemo {
		if !isDevMode(c.Mode) {
			cancel()
			_ = db.Close()
			return nil, errors.New("demo seeding is only allowed in dev/test mode")
		}
		demoPassword := strings.TrimSpace(os.Getenv("CITYFLOW_DEMO_PASSWORD"))
		if len(demoPassword) < 8 {
			cancel()
			_ = db.Close()
			return nil, errors.New("CITYFLOW_DEMO_PASSWORD must be at least 8 characters when CITYFLOW_SEED_DEMO=true")
		}
		if err := store.SeedDemoIdentity(ctx, demoPassword); err != nil {
			cancel()
			_ = db.Close()
			return nil, err
		}
	}
	cancel()
	accounts = store
	outboxStore = identitymysql.OutboxStore{Store: store}
	closeFn = db.Close
	pingFn = db.Ping

	// Identity owns accounts only. Merchant and rider profile ownership moves
	// out of this service; the methods that used them are not exposed by the
	// Identity RPC.
	authService := authservice.NewAuthService(accounts, []byte(secret))

	var users *userservice.UserService
	if managed, ok := accounts.(repository.UserRepository); ok {
		users = userservice.NewUserService(accounts, managed)
		// Returning the reset code to the caller is a local/demo convenience only.
		// It is opt-in and refused outside dev/test: anyone who can reach the
		// public reset endpoint could otherwise take over any account.
		exposeReset := strings.TrimSpace(os.Getenv("CITYFLOW_EXPOSE_RESET_TOKEN")) == "true"
		if exposeReset && !isDevMode(c.Mode) {
			log.Fatalf("CITYFLOW_EXPOSE_RESET_TOKEN=true is only allowed in dev/test mode")
		}
		users = users.WithResetTokenExposure(exposeReset)
	}
	var registrations *riderregservice.RiderRegistrationService
	if regRepo, ok := accounts.(repository.RiderRegistrationRepository); ok {
		registrations = riderregservice.NewRiderRegistrationService(regRepo)
	}

	var refresh authservice.RefreshStore
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
		refresh = identityredis.NewRefreshStore(redis)
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
	var relay *outboxservice.OutboxRelay
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
			log.Printf("warning: RabbitMQ unavailable (%v); rider approval events will stay pending", probeErr)
			_ = publisher.Close()
			publisher = nil
		} else {
			relay = outboxservice.NewOutboxRelay(outboxStore, publisher, 2*time.Second, 50)
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

	jobRuntime, err := xxljob.NewRuntime(c.XXLJob.WithEnv("IDENTITY"), nil)
	if err != nil {
		_ = closeFn()
		return nil, err
	}

	return &ServiceContext{
		Config:    c,
		Auth:      authService,
		Refresh:   refresh,
		Secret:    []byte(secret),
		Users:     users,
		Riders:    registrations,
		Relay:     relay,
		XXLJob:    jobRuntime,
		accounts:  accounts,
		publisher: publisher,
		closeFn:   closeFn,
		pingFn:    pingFn,
	}, nil
}

// AccountActive is the narrow identity check the RPC interceptor needs. It
// deliberately does not expose the repository to transport code.
func (s *ServiceContext) AccountActive(ctx context.Context, subject, _ string) (bool, error) {
	if s.accounts == nil {
		return false, errors.New("identity account store is unavailable")
	}
	return s.accounts.IsActive(ctx, subject)
}

func (s *ServiceContext) Close() error {
	if s.XXLJob != nil {
		s.XXLJob.Stop()
	}
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

func isDevMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "dev", "test":
		return true
	default:
		return false
	}
}
