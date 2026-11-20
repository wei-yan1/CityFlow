package svc

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	"cityflow/app/payment/cmd/rpc/internal/config"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/application/repository"
	"cityflow/internal/application/service"
	"cityflow/internal/infrastructure/mq"
	mysqlstore "cityflow/internal/infrastructure/mysql"
	"cityflow/internal/infrastructure/payment/settings"
	"cityflow/internal/platform/rpcadapt"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServiceContext struct {
	Config config.Config

	Orders         repository.PaymentRepository
	Store          *mysqlstore.Store
	Coupons        repository.CouponRepository
	Payments       *service.PaymentService
	CouponSvc      *service.CouponService
	WorkerInterval time.Duration

	Identity identityclient.Identity
	Task     taskclient.Task
	Consumer *mq.Consumer
	Secret   []byte

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
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		secret = "CHANGE_ME_JWT_SECRET"
		log.Printf("warning: JWT secret is not set; using development secret")
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

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("PAYMENT_DATABASE_DSN"))
	}
	if dsn == "" {
		return nil, errors.New("payment datasource is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := mysqlstore.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := store.MigrateService(ctx, "payment"); err != nil {
		store.Close()
		return nil, err
	}

	tasks := rpcadapt.Tasks{Client: task}
	payments := service.NewPaymentService(store, tasks, channels.Gateways(), channels.Provider).
		WithExpiry(channels.Expiry).
		WithFeeBPS(channels.FeeBPS).
		WithMockPay(channels.AllowMockPay).
		WithInbox(store)
	if batch, ok := any(store).(repository.PaymentNotifyBatchWriter); ok {
		payments.WithNotifyBuffer(service.NewNotifyLogBuffer(
			batch.RecordNotifyBatch,
			store.RecordNotify,
			1024, 100, 100*time.Millisecond,
		))
	}
	coupons := service.NewCouponService(store, tasks)
	payments.WithCoupons(coupons)

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

	return &ServiceContext{
		Config: c, Orders: store, Coupons: store,
		Store: store, Payments: payments, CouponSvc: coupons, WorkerInterval: channels.SettleInterval,
		Identity: identity, Task: task, Consumer: consumer, Secret: []byte(secret),
		closeFn: func() error { store.Close(); return nil },
		pingFn:  store.Ping,
	}, nil
}

func (s *ServiceContext) Close() error {
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
