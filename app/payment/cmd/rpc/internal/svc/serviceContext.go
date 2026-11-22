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
	paymentservice "cityflow/app/payment/application/payment"
	"cityflow/app/payment/cmd/rpc/internal/config"
	paymentmysql "cityflow/app/payment/internal/adapter/mysql"
	"cityflow/app/payment/internal/adapter/settings"
	"cityflow/app/payment/repository"
	promotionclient "cityflow/app/promotion/cmd/rpc/promotionclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/integration/rpcadapt"
	"cityflow/internal/platform/mq"
	platformmysql "cityflow/internal/platform/mysql"
	"cityflow/internal/platform/rpcctx"
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServiceContext struct {
	Config config.Config

	Orders          repository.PaymentRepository
	Store           *paymentmysql.Store
	Payments        *paymentservice.PaymentService
	WorkerInterval  time.Duration
	XXLJob          *xxljob.Runtime
	NotifyRetention time.Duration

	Identity  identityclient.Identity
	Task      taskclient.Task
	Promotion promotionclient.Promotion
	Consumer  *mq.Consumer
	Secret    []byte

	closeFn func() error
	pingFn  func(context.Context) error
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	channels, err := settings.Load(settings.PaymentConfig{
		Provider: strings.ToUpper(strings.TrimSpace(c.Provider)), MockBaseURL: strings.TrimSpace(c.MockBaseURL),
		FeeBPS: c.FeeBPS, Expiry: time.Duration(c.ExpiryMinutes) * time.Minute,
		SettleInterval: time.Duration(c.SettleIntervalSeconds) * time.Second, AllowMockPay: c.AllowMockPay,
	})
	if err != nil {
		return nil, err
	}
	if !isDevMode(c.Mode) {
		if strings.EqualFold(strings.TrimSpace(channels.Provider), "MOCK") {
			log.Fatalf("payment provider MOCK is only allowed in dev/test mode")
		}
		if channels.AllowMockPay {
			log.Fatalf("AllowMockPay=true is only allowed in dev/test mode")
		}
	}
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		log.Fatalf("JWT secret is not configured: set CITYFLOW_JWT_SECRET in .env")
	}

	var identity identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identity = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}
	var task taskclient.Task
	if len(c.TaskRPC.Endpoints) > 0 {
		task = taskclient.NewTask(zrpc.MustNewClient(c.TaskRPC))
	}
	if task == nil {
		return nil, errors.New("TaskRPC is required by the payment service")
	}
	var promotion promotionclient.Promotion
	if len(c.PromotionRPC.Endpoints) > 0 {
		promotion = promotionclient.NewPromotion(zrpc.MustNewClient(c.PromotionRPC))
	}

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("PAYMENT_DATABASE_DSN"))
	}
	if dsn == "" {
		return nil, errors.New("payment datasource is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := platformmysql.MigrateService(ctx, db, "payment"); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := paymentmysql.New(db)

	tasks := paymentTasks{tasks: rpcadapt.Tasks{Client: task}}
	payments := paymentservice.NewPaymentService(store, tasks, channels.Gateways(), channels.Provider).
		WithExpiry(channels.Expiry).
		WithFeeBPS(channels.FeeBPS).
		WithMockPay(channels.AllowMockPay).
		WithInbox(store)
	if batch, ok := any(store).(repository.PaymentNotifyBatchWriter); ok {
		payments.WithNotifyBuffer(paymentservice.NewNotifyLogBuffer(
			batch.RecordNotifyBatch,
			store.RecordNotify,
			1024, 100, 100*time.Millisecond,
		))
	}
	if promotion != nil {
		payments.WithCoupons(promotionCouponGate{tasks: task, promotion: promotion})
	} else {
		log.Printf("warning: PromotionRPC is empty; coupon discounts are disabled")
	}

	var consumer *mq.Consumer
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL != "" {
		consumer = mq.NewConsumer(mqURL, "payment.task-events", "task.completed.v1", "task.cancelled.v1")
	} else {
		log.Printf("warning: RABBITMQ_URL is empty; payment consumer will not start")
	}

	notifyRetention := time.Duration(c.NotifyRetentionDays) * 24 * time.Hour
	if notifyRetention <= 0 {
		notifyRetention = 365 * 24 * time.Hour
	}
	if raw := strings.TrimSpace(os.Getenv("PAYMENT_NOTIFY_RETENTION_DAYS")); raw != "" {
		if days, parseErr := strconv.Atoi(raw); parseErr == nil && days > 0 {
			notifyRetention = time.Duration(days) * 24 * time.Hour
		} else {
			log.Printf("warning: invalid PAYMENT_NOTIFY_RETENTION_DAYS=%q; using %d days", raw, c.NotifyRetentionDays)
		}
	}

	jobRuntime, err := xxljob.NewRuntime(c.XXLJob.WithEnv("PAYMENT"), nil)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &ServiceContext{
		Config: c, Orders: store,
		Store: store, Payments: payments, WorkerInterval: channels.SettleInterval,
		XXLJob: jobRuntime, NotifyRetention: notifyRetention,
		Identity: identity, Task: task, Promotion: promotion, Consumer: consumer, Secret: []byte(secret),
		closeFn: db.Close,
		pingFn:  db.Ping,
	}, nil
}

func (s *ServiceContext) Close() error {
	if s.XXLJob != nil {
		s.XXLJob.Stop()
	}
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func (s *ServiceContext) Ping(ctx context.Context) error {
	if s.pingFn != nil {
		return s.pingFn(ctx)
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

func isDevMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "dev", "test":
		return true
	default:
		return false
	}
}
