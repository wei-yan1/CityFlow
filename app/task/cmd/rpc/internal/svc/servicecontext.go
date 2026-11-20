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
	"cityflow/app/identity/cmd/rpc/identityclient"
	paymentclient "cityflow/app/payment/cmd/rpc/paymentclient"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	"cityflow/app/task/cmd/rpc/internal/config"
	trackingclient "cityflow/app/tracking/cmd/rpc/trackingclient"
	"cityflow/internal/application/repository"
	"cityflow/internal/application/service"
	"cityflow/internal/domain"
	"cityflow/internal/domain/payment"
	"cityflow/internal/domain/reaction"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
	"cityflow/internal/infrastructure/maps"
	"cityflow/internal/infrastructure/memory"
	"cityflow/internal/infrastructure/mq"
	mysqlstore "cityflow/internal/infrastructure/mysql"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcadapt"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// accountPort is the subset of identity facts Task needs; satisfied by the
// local memory store in tests or by an RPC adapter in the microservice layout.
type accountPort interface {
	GetUser(ctx context.Context, id string) (*user.User, error)
	IsActive(ctx context.Context, id string) (bool, error)
}

type ServiceContext struct {
	Config    config.Config
	Tasks     *service.TaskService
	Riders    *service.RiderService
	Feedback  *service.RiderFeedbackService
	Comments  *service.CommentService
	RiderRepo repository.RiderRepository
	Accounts  accountPort
	Identity  identityclient.Identity
	Secret    []byte
	Status    *authn.StatusCache
	Relay     *service.OutboxRelay
	Consumer  *mq.Consumer
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
		secret = "CHANGE_ME_JWT_SECRET"
		log.Printf("warning: JWT secret is not set; using development secret")
	}

	var identityClient identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identityClient = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}
	var discoveryClient discoveryclient.Discovery
	if len(c.DiscoveryRPC.Endpoints) > 0 {
		discoveryClient = discoveryclient.NewDiscovery(zrpc.MustNewClient(c.DiscoveryRPC))
	}
	var trackingClient trackingclient.Tracking
	if len(c.TrackingRPC.Endpoints) > 0 {
		trackingClient = trackingclient.NewTracking(zrpc.MustNewClient(c.TrackingRPC))
	}
	var paymentClient paymentclient.Payment
	if len(c.PaymentRPC.Endpoints) > 0 {
		paymentClient = paymentclient.NewPayment(zrpc.MustNewClient(c.PaymentRPC))
	}
	var reactionClient reactionclient.Reaction
	if len(c.ReactionRPC.Endpoints) > 0 {
		reactionClient = reactionclient.NewReaction(zrpc.MustNewClient(c.ReactionRPC))
	}

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("TASK_DATABASE_DSN"))
	}

	var (
		store       repository.TaskRepository
		full        any
		accounts    accountPort
		orders      repository.PaymentRepository
		trusted     repository.TrustedPlaces
		comments    repository.ReviewComments
		outboxStore repository.TaskOutboxRepository
		closeFn     = func() error { return nil }
		pingFn      = func(context.Context) error { return nil }
	)
	if dsn == "" {
		memoryStore, err := memory.NewStore()
		if err != nil {
			return nil, err
		}
		store, accounts, orders, trusted, full = memoryStore, memoryStore, memoryStore, nil, memoryStore
		comments = memory.RiderComments{Store: memoryStore}
		closeFn = memoryStore.Close
	} else {
		if paymentClient == nil {
			return nil, errors.New("PaymentRPC is required when the task service uses MySQL")
		}
		if discoveryClient == nil {
			return nil, errors.New("DiscoveryRPC is required when the task service uses MySQL")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		mysqlStore, err := mysqlstore.Open(ctx, dsn)
		if err != nil {
			cancel()
			return nil, err
		}
		if err := mysqlStore.MigrateService(ctx, "task"); err != nil {
			cancel()
			mysqlStore.Close()
			return nil, err
		}
		cancel()
		store, full = mysqlStore, mysqlStore
		accounts = nil
		orders = nil
		trusted = nil
		comments = mysqlstore.RiderComments{Store: mysqlStore}
		outboxStore = mysqlstore.TaskOutboxStore{Store: mysqlStore}
		closeFn = func() error { mysqlStore.Close(); return nil }
		pingFn = mysqlStore.Ping
	}

	taskService := service.NewTaskService(store)
	if outboxStore != nil {
		taskService.WithOutbox(outboxStore)
	}
	ttl := c.TrustedPlaceTTL
	if ttl <= 0 {
		ttl = 1800
	}
	if ttl > 604800 {
		return nil, errors.New("TrustedPlaceTTL must be at most 604800 seconds")
	}
	// Cross-service reads go through RPC adapters once the owning service is
	// configured; the local read-only views remain only as a transition fallback.
	if discoveryClient != nil {
		trusted = rpcadapt.Trusted{Client: discoveryClient}
	}
	if trusted != nil {
		taskService.WithTrustedPlaces(service.TrustedPlaces{
			Store:    trusted,
			Lifetime: time.Duration(ttl) * time.Second,
		}, maps.GCJ02ToWGS84)
	}
	if paymentClient != nil {
		taskService.WithPaymentGate(rpcadapt.PaymentGate{Client: paymentClient})
		taskService.WithSettlements(rpcadapt.Settlements{Client: paymentClient})
	} else {
		taskService.WithPaymentGate(paymentGate{orders: orders})
	}

	// Rider performance profiles and rider feedback are owned by Task.
	var riders *service.RiderService
	var feedback *service.RiderFeedbackService
	commentSvc := service.NewCommentService(comments, true)
	commentSvc.AnonymousLabel = "匿名用户" // matches the existing rider public projection
	commentSvc.ReactionTarget = reaction.TargetRiderComment
	if reactionClient != nil {
		commentSvc.Reactions = rpcadapt.ReactionReader{Client: reactionClient}
	}
	var riderRepoVar repository.RiderRepository
	if accounts == nil && identityClient != nil {
		accounts = rpcadapt.Accounts{Client: identityClient}
	}
	if riderRepo, ok := full.(repository.RiderRepository); ok {
		riderRepoVar = riderRepo
		riders = service.NewRiderService(riderRepo, accounts, taskService)
		if trackingClient != nil {
			riders.WithPositions(rpcadapt.Positions{Client: trackingClient})
		}
		if feedbackRepo, ok := full.(repository.RiderFeedback); ok {
			feedback = service.NewRiderFeedbackService(feedbackRepo, store, riderRepo, accounts)
			feedback.ReactionTarget = reaction.TargetRiderReview
			if reactionClient != nil {
				feedback.Reactions = rpcadapt.ReactionReader{Client: reactionClient}
			}
		}
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
	statusCache := authn.NewStatusCache(statusTTL, func(callCtx context.Context, subject, token string) (bool, error) {
		if identityClient != nil {
			reply, err := identityClient.Verify(rpcctx.WithBearer(callCtx, token), &identityclient.VerifyRequest{})
			if err != nil {
				if status.Code(err) == codes.Unauthenticated {
					return false, nil
				}
				return false, err
			}
			return reply.GetActive() && reply.GetSubject() == subject, nil
		}
		if accounts != nil {
			return accounts.IsActive(callCtx, subject)
		}
		return false, errors.New("identity verifier is unavailable")
	})

	var publisher *mq.Publisher
	var relay *service.OutboxRelay
	var consumer *mq.Consumer
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL != "" && outboxStore != nil {
		publisher = mq.NewPublisher(mqURL)
		relay = service.NewOutboxRelay(outboxStore, publisher, 2*time.Second, 50)
		consumer = mq.NewConsumer(mqURL, "task.rider-events", "rider.approved.v1")
		previousClose := closeFn
		closeFn = func() error {
			err := previousClose()
			if pubErr := publisher.Close(); err == nil {
				err = pubErr
			}
			return err
		}
	} else if outboxStore != nil {
		log.Printf("warning: RABBITMQ_URL is empty; task outbox will stay pending")
	}

	return &ServiceContext{
		Config:    c,
		Tasks:     taskService,
		Riders:    riders,
		Comments:  commentSvc,
		Feedback:  feedback,
		RiderRepo: riderRepoVar,
		Accounts:  accounts,
		Identity:  identityClient,
		Secret:    []byte(secret),
		Status:    statusCache,
		Relay:     relay,
		Consumer:  consumer,
		publisher: publisher,
		closeFn:   closeFn,
		pingFn:    pingFn,
	}, nil
}

// AccountActive checks the account through a short-TTL cache. Task deliberately
// has no users table in its own schema; the local memory fallback is only for
// isolated tests. Downstream write logic still owns its own task permissions.
func (s *ServiceContext) AccountActive(ctx context.Context, subject, token string) (bool, error) {
	if s.Status == nil {
		return false, errors.New("identity verifier is unavailable")
	}
	return s.Status.IsActive(ctx, subject, token)
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
		return errors.Join(errors.New("task database unavailable"), err)
	}
	return nil
}

type paymentGate struct {
	orders repository.PaymentRepository
}

func (g paymentGate) EnsureClaimable(ctx context.Context, item *task.Task) error {
	if item == nil || item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
		return nil
	}
	order, err := g.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: 请先完成支付后再接单", domain.ErrConflict)
	}
	if err != nil {
		return err
	}
	if order.Status != payment.PaymentPaid {
		return fmt.Errorf("%w: 任务尚未收到付款，暂不能接单", domain.ErrConflict)
	}
	return nil
}

func (g paymentGate) EnsureEditable(ctx context.Context, item *task.Task, nextAmount *int64) error {
	if item == nil || item.QuotedAmountFen == nil {
		return nil
	}
	order, err := g.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if order.Status != payment.PaymentPending && order.Status != payment.PaymentPaid {
		return nil
	}
	if nextAmount == nil || *nextAmount != order.AmountFen {
		return fmt.Errorf("%w: 已发起支付的任务不能修改报价", domain.ErrConflict)
	}
	return nil
}

func (g paymentGate) AfterCancel(context.Context, *task.Task) {}

var _ service.PaymentGate = paymentGate{}
