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

	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	"cityflow/app/reaction/cmd/rpc/internal/config"
	"cityflow/internal/domain/reaction"
	"cityflow/internal/infrastructure/cache"
	"cityflow/internal/infrastructure/mq"
	mysqlstore "cityflow/internal/infrastructure/mysql"
	"cityflow/internal/platform/messaging"
	"cityflow/internal/platform/rpcctx"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServiceContext struct {
	Config    config.Config
	Store     mysqlstore.ReactionStore
	Cache     *cache.ReactionCache
	Publisher *mq.Publisher
	Identity  identityclient.Identity
	Secret    []byte
	Redis     *redis.Client

	closeFn func() error
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
		dsn = strings.TrimSpace(c.ReactionDSN())
	}
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("REACTION_DATABASE_DSN"))
	}
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	}
	if dsn == "" {
		return nil, errors.New("reaction MySQL DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := mysqlstore.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := store.MigrateService(ctx, "reaction"); err != nil {
		store.Close()
		return nil, err
	}
	reactionStore := mysqlstore.ReactionStore{Store: store}

	redisAddr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if redisAddr == "" {
		redisAddr = strings.TrimSpace(c.ReactionRedis.Host)
	}
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6380"
	}
	redisPassword := strings.TrimSpace(os.Getenv("REDIS_PASSWORD"))
	if redisPassword == "" {
		redisPassword = c.ReactionRedis.Password
	}
	redisDB := c.ReactionRedis.DB
	if raw := strings.TrimSpace(os.Getenv("REDIS_DB")); raw != "" {
		if value, parseErr := strconv.Atoi(raw); parseErr == nil {
			redisDB = value
		}
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr, Password: redisPassword, DB: redisDB,
		DialTimeout: 500 * time.Millisecond, ReadTimeout: 300 * time.Millisecond,
		WriteTimeout: 300 * time.Millisecond, PoolTimeout: 500 * time.Millisecond,
		MaxRetries: 1, PoolSize: 30,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		redisClient.Close()
		store.Close()
		return nil, fmt.Errorf("reaction redis: %w", err)
	}

	var publisher *mq.Publisher
	mqURL := strings.TrimSpace(c.RabbitMQ.URL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL != "" {
		exchange := strings.TrimSpace(c.RabbitMQ.Exchange)
		if exchange == "" {
			exchange = "cf.events"
		}
		publisher = mq.NewPublisherWithExchange(mqURL, exchange)
	}

	var identity identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identity = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}

	reactionCache := cache.NewReactionCache(redisClient)
	reactionCache.SetProjectionTypes(c.ProjectionTypes)
	ctxsvc := &ServiceContext{
		Config: c, Store: reactionStore, Cache: reactionCache,
		Publisher: publisher, Identity: identity, Secret: []byte(secret), Redis: redisClient,
		closeFn: func() error {
			if publisher != nil {
				_ = publisher.Close()
			}
			_ = redisClient.Close()
			store.Close()
			return nil
		},
	}
	return ctxsvc, nil
}

func (s *ServiceContext) Close() error {
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func (s *ServiceContext) AccountActive(ctx context.Context, subject, token string) (bool, error) {
	if s.Identity == nil {
		return false, errors.New("identity service is unavailable")
	}
	reply, err := s.Identity.Verify(rpcctx.WithBearer(ctx, token), &identityclient.VerifyRequest{})
	if err != nil {
		if status.Code(err) == codes.Unauthenticated {
			return false, nil
		}
		return false, err
	}
	return reply.GetActive() && reply.GetSubject() == subject, nil
}

// StartWorkers runs the two background loops in the RPC process. Only
// WorkersEnabled instances take work, so multiple RPC replicas can scale reads
// while exactly one owns projection and persistence.
func (s *ServiceContext) StartWorkers(ctx context.Context) {
	if !s.Config.WorkersEnabled {
		return
	}
	types := s.Config.ProjectionTypes
	if len(types) == 0 {
		types = []string{reaction.TargetPlaceReview}
	}
	for _, targetType := range types {
		go s.runProjection(ctx, targetType)
	}
	go s.runStreamWorker(ctx)
}

func (s *ServiceContext) runProjection(ctx context.Context, targetType string) {
	interval := time.Duration(s.Config.TickerSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	batch := int64(s.Config.TickerBatchSize)
	if batch <= 0 {
		batch = 1000
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.projectOnce(ctx, targetType, batch)
		}
	}
}

func (s *ServiceContext) projectOnce(ctx context.Context, targetType string, batch int64) {
	items, err := s.Cache.PopProjection(ctx, targetType, batch)
	if err != nil {
		log.Printf("reaction projection %s: %v", targetType, err)
		return
	}
	if len(items) == 0 {
		return
	}
	payload, err := cache.ProjectionMessage(targetType, items)
	if err != nil {
		_ = s.Cache.RequeueProjection(ctx, targetType, items)
		return
	}
	if s.Publisher == nil {
		// Without a broker, mysql is still the durable source; put the values
		// back so a later broker-enabled run can project them.
		_ = s.Cache.RequeueProjection(ctx, targetType, items)
		return
	}
	msg := messaging.Message{
		ID:   fmt.Sprintf("like-%s-%d", targetType, time.Now().UTC().UnixNano()),
		Type: "like.times." + targetType, AggregateID: targetType, Payload: payload,
		OccurredAt: time.Now().UTC(),
	}
	if err := s.Publisher.Publish(ctx, msg); err != nil {
		_ = s.Cache.RequeueProjection(ctx, targetType, items)
		log.Printf("reaction projection publish %s: %v", targetType, err)
	}
}

func (s *ServiceContext) runStreamWorker(ctx context.Context) {
	batch := int64(s.Config.OpsBatchSize)
	if batch <= 0 {
		batch = 1000
	}
	block := time.Duration(s.Config.OpsBlockMillis) * time.Millisecond
	if block <= 0 {
		block = 200 * time.Millisecond
	}
	consumer := fmt.Sprintf("%s-%d", hostname(), os.Getpid())
	for {
		if ctx.Err() != nil {
			return
		}
		ops, err := s.Cache.ReadOps(ctx, consumer, batch, block)
		if err != nil {
			log.Printf("reaction stream worker: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		if len(ops) == 0 {
			continue
		}
		domainOps := make([]reaction.Op, 0, len(ops))
		ids := make([]string, 0, len(ops))
		for _, op := range ops {
			domainOps = append(domainOps, reaction.Op{
				TargetType: op.TargetType, TargetID: op.TargetID, UserID: op.UserID,
				Kind: op.Kind, Active: op.Active,
			})
			ids = append(ids, op.MessageID)
		}
		if err := s.Store.Apply(ctx, domainOps); err != nil {
			log.Printf("reaction stream apply: %v", err)
			continue
		}
		if err := s.Cache.AckOps(ctx, ids...); err != nil {
			log.Printf("reaction stream ack: %v", err)
		}
	}
}

func (s *ServiceContext) Health(ctx context.Context) (bool, error) {
	if err := s.Redis.Ping(ctx).Err(); err != nil {
		return false, err
	}
	if err := s.Store.Ping(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "reaction"
	}
	return name
}
