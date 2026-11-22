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

	"cityflow/app/promotion/cmd/rpc/internal/config"
	promotionadapter "cityflow/app/promotion/internal/adapter/mysql"
	promotionredis "cityflow/app/promotion/internal/adapter/redis"
	"cityflow/app/promotion/internal/application/promotionapp"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/cache"
	platformmysql "cityflow/internal/platform/mysql"
	"cityflow/internal/platform/xxljob"
)

type ServiceContext struct {
	Config      config.Config
	Secret      []byte
	DB          *platformmysql.DB
	Store       *promotionadapter.Store
	Service     *promotionapp.Service
	CouponCache *promotionredis.Cache
	XXLJob      *xxljob.Runtime
	redis       *cache.Redis
	workers     []*promotionapp.ClaimWorker
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("PROMOTION_DATABASE_DSN"))
	}
	if dsn == "" {
		return nil, errors.New("PROMOTION_DATABASE_DSN is required")
	}
	db, err := platformmysql.Open(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("open promotion database: %w", err)
	}
	if err := platformmysql.MigrateService(context.Background(), db, "promotion"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate promotion database: %w", err)
	}
	store := promotionadapter.New(db)
	service := promotionapp.New(store)

	// Redis 是可选加速层：配置了就必须能连上；连不上只降级，不阻断整个领券服务。
	var redisCache *cache.Redis
	var couponCache *promotionredis.Cache
	address := strings.TrimSpace(c.RedisAddr)
	if address == "" {
		address = strings.TrimSpace(os.Getenv("PROMOTION_REDIS_ADDR"))
	}
	if address == "" {
		address = strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	}
	if address != "" {
		password := strings.TrimSpace(c.RedisPassword)
		if password == "" {
			password = strings.TrimSpace(os.Getenv("REDIS_PASSWORD"))
		}
		dbIndex := c.RedisDB
		if raw := strings.TrimSpace(os.Getenv("PROMOTION_REDIS_DB")); raw != "" {
			if value, convErr := strconv.Atoi(raw); convErr == nil {
				dbIndex = value
			}
		}
		redisCache = cache.NewRedis(address, password, dbIndex)
		pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr := redisCache.Ping(pingCtx)
		cancel()
		if pingErr != nil {
			log.Printf("warning: promotion coupon cache disabled: %v", pingErr)
			_ = redisCache.Close()
			redisCache = nil
		} else {
			couponCache = promotionredis.New(redisCache.Client())
			service = service.WithGate(couponCache)
		}
	}

	// reserve 模式：Redis 原子预扣 + Stream 异步落库；没有 Redis 就退回 sync，不阻断服务。
	claimMode := strings.ToLower(strings.TrimSpace(c.ClaimMode))
	if raw := strings.ToLower(strings.TrimSpace(os.Getenv("PROMOTION_CLAIM_MODE"))); raw != "" {
		claimMode = raw
	}
	var workers []*promotionapp.ClaimWorker
	if claimMode == "reserve" {
		if couponCache == nil {
			log.Printf("warning: PROMOTION_CLAIM_MODE=reserve requires Redis; falling back to sync")
		} else {
			resultTTL := time.Duration(c.ClaimResultTTLSeconds) * time.Second
			stream := promotionredis.NewClaimStream(redisCache.Client(), c.ClaimStream, c.ClaimGroup, resultTTL)
			wait := time.Duration(c.ClaimWaitMS) * time.Millisecond
			if wait <= 0 {
				wait = 300 * time.Millisecond
			}
			service = service.WithAsyncClaimer(promotionapp.NewAsyncClaimer(store, stream, wait))
			count := c.ClaimWorkers
			if count <= 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				consumer := fmt.Sprintf("promotion-%d-%d", os.Getpid(), i)
				workers = append(workers, promotionapp.NewClaimWorker(
					store, stream, stream, consumer, c.ClaimBatch,
					time.Second, time.Duration(c.ClaimReclaimIdleSeconds)*time.Second,
				))
			}
		}
	}

	jobRuntime, err := xxljob.NewRuntime(c.XXLJob.WithEnv("PROMOTION"), nil)
	if err != nil {
		if redisCache != nil {
			_ = redisCache.Close()
		}
		_ = db.Close()
		return nil, err
	}
	return &ServiceContext{
		Config: c, Secret: authn.ResolveSecret(c.JWTSecret), DB: db, Store: store, Service: service,
		CouponCache: couponCache, XXLJob: jobRuntime, redis: redisCache, workers: workers,
	}, nil
}

// StartWorkers 启动 Stream 消费者；ctx 取消时全部退出。
func (s *ServiceContext) StartWorkers(ctx context.Context) {
	if s == nil {
		return
	}
	for _, worker := range s.workers {
		go worker.Run(ctx)
	}
}

func (s *ServiceContext) Close() error {
	if s == nil {
		return nil
	}
	var errs []error
	if s.XXLJob != nil {
		s.XXLJob.Stop()
	}
	if s.redis != nil {
		errs = append(errs, s.redis.Close())
	}
	if s.DB != nil {
		errs = append(errs, s.DB.Close())
	}
	return errors.Join(errs...)
}
