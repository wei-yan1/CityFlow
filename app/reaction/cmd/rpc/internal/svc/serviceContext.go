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
	reactionservice "cityflow/app/reaction/application/reaction"
	"cityflow/app/reaction/cmd/rpc/internal/config"
	reactionredis "cityflow/app/reaction/internal/adapter/redis"
	"cityflow/internal/common/contract"
	"cityflow/internal/platform/cache"
	"cityflow/internal/platform/mq"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServiceContext struct {
	Config    config.Config
	Reaction  *reactionservice.Service
	Publisher *mq.Publisher
	Identity  identityclient.Identity
	Secret    []byte
	redis     *cache.Redis
	closeFn   func() error
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		log.Fatalf("JWT secret is not configured: set CITYFLOW_JWT_SECRET in .env")
	}

	address := strings.TrimSpace(c.RedisAddr)
	if address == "" {
		address = strings.TrimSpace(os.Getenv("REACTION_REDIS_ADDR"))
	}
	if address == "" {
		address = strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	}
	if address == "" {
		address = "127.0.0.1:6380"
	}
	password := strings.TrimSpace(c.RedisPassword)
	if password == "" {
		password = strings.TrimSpace(os.Getenv("REDIS_PASSWORD"))
	}
	dbIndex := c.RedisDB
	if raw := strings.TrimSpace(os.Getenv("REACTION_REDIS_DB")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			dbIndex = value
		}
	}
	redisCache := cache.NewRedis(address, password, dbIndex)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisCache.Ping(ctx); err != nil {
		_ = redisCache.Close()
		return nil, err
	}

	var identity identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identity = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}

	var publisher *mq.Publisher
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL == "" {
		return nil, errors.New("RabbitMQURL is required: like counts must be projected to content services")
	}
	exchange := strings.TrimSpace(c.LikeExchange)
	if exchange == "" {
		exchange = "like.record.topic"
	}
	publisher = mq.NewPublisherWithExchange(mqURL, exchange)

	s := &ServiceContext{
		Config:    c,
		Reaction:  reactionservice.New(reactionredis.New(redisCache.Client())),
		Publisher: publisher,
		Identity:  identity,
		Secret:    []byte(secret),
		redis:     redisCache,
		closeFn: func() error {
			if publisher != nil {
				_ = publisher.Close()
			}
			return redisCache.Close()
		},
	}
	return s, nil
}

func (s *ServiceContext) Close() error {
	if s != nil && s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func (s *ServiceContext) StartWorkers(ctx context.Context) {
	if s == nil || s.Reaction == nil || s.Publisher == nil {
		return
	}
	types := s.Config.ProjectionTypes
	if len(types) == 0 {
		types = []string{
			contract.TargetPlaceReview,
			contract.TargetPlaceComment,
			contract.TargetRiderReview,
			contract.TargetRiderComment,
		}
	}
	interval := time.Duration(s.Config.TickerSeconds) * time.Second
	if interval <= 0 {
		interval = 20 * time.Second
	}
	go s.Reaction.RunProjector(ctx, types, s.Publisher, interval, s.Config.TickerBatchSize)
}

func (s *ServiceContext) Ping(ctx context.Context) error {
	if s == nil || s.Reaction == nil {
		return errors.New("reaction service is not configured")
	}
	if err := s.Reaction.Health(ctx); err != nil {
		return err
	}
	if s.Publisher == nil {
		return errors.New("like projection publisher is not configured")
	}
	if err := s.Publisher.Probe(ctx); err != nil {
		return fmt.Errorf("like projection rabbitmq: %w", err)
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
