package svc

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/app/identity/cmd/rpc/identityclient"
	paymentclient "cityflow/app/payment/cmd/rpc/paymentclient"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	commentservice "cityflow/app/task/application/comment"
	feedbackservice "cityflow/app/task/application/feedback"
	"cityflow/app/task/application/ports"
	riderservice "cityflow/app/task/application/rider"
	taskservice "cityflow/app/task/application/task"
	"cityflow/app/task/cmd/rpc/internal/config"
	"cityflow/app/task/internal/adapter/discoveryrpc"
	taskmysql "cityflow/app/task/internal/adapter/mysql"
	"cityflow/app/task/internal/adapter/paymentrpc"
	"cityflow/app/task/repository"
	trackingclient "cityflow/app/tracking/cmd/rpc/trackingclient"
	"cityflow/internal/common/contract"
	"cityflow/internal/integration/rpcadapt"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/maps"
	"cityflow/internal/platform/mq"
	platformmysql "cityflow/internal/platform/mysql"
	outboxservice "cityflow/internal/platform/outboxservice"
	"cityflow/internal/platform/rpcctx"
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// accountPort is the subset of identity facts Task needs; satisfied by the
// local memory store in tests or by an RPC adapter in the microservice layout.
type accountPort interface {
	GetAccount(ctx context.Context, id string) (*ports.Account, error)
	IsActive(ctx context.Context, id string) (bool, error)
}

// identityAccounts adapts the shared identity RPC projection to Task's port.
type identityAccounts struct{ client identityclient.Identity }

func (a identityAccounts) GetAccount(ctx context.Context, id string) (*ports.Account, error) {
	account, err := rpcadapt.Accounts{Client: a.client}.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ports.Account{
		ID: account.ID, Email: account.Email, Name: account.Name,
		AvatarURL: account.AvatarURL, Role: account.Role, Status: account.Status,
	}, nil
}

func (a identityAccounts) IsActive(ctx context.Context, id string) (bool, error) {
	account, err := a.GetAccount(ctx, id)
	if err != nil {
		return false, err
	}
	return account.Status == "ACTIVE", nil
}

type ServiceContext struct {
	Config       config.Config
	Tasks        *taskservice.TaskService
	Riders       *riderservice.RiderService
	Feedback     *feedbackservice.RiderFeedbackService
	Comments     *commentservice.CommentService
	RiderRepo    repository.RiderRepository
	Reactions    rpcadapt.Reactions
	LikeCounts   repository.LikeProjection
	Accounts     accountPort
	Identity     identityclient.Identity
	Discovery    discoveryclient.Discovery
	Secret       []byte
	Status       *authn.StatusCache
	Relay        *outboxservice.OutboxRelay
	Consumer     *mq.Consumer
	LikeConsumer *mq.Consumer
	XXLJob       *xxljob.Runtime
	// InternalJobToken authenticates scheduler-only calls to Discovery.
	InternalJobToken string
	publisher        *mq.Publisher
	closeFn          func() error
	pingFn           func(context.Context) error
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	secret := strings.TrimSpace(c.JWTSecret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		log.Fatalf("JWT secret is not configured: set CITYFLOW_JWT_SECRET in .env")
	}
	internalJobToken := strings.TrimSpace(c.InternalJobToken)
	if internalJobToken == "" {
		internalJobToken = strings.TrimSpace(os.Getenv("CITYFLOW_INTERNAL_JOB_TOKEN"))
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
	if reactionClient == nil {
		return nil, errors.New("ReactionRPC is required when the task service uses MySQL")
	}

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("TASK_DATABASE_DSN"))
	}

	var (
		store       repository.TaskRepository
		full        any
		accounts    accountPort
		comments    repository.ReviewComments
		outboxStore repository.TaskOutboxRepository
		likeCounts  repository.LikeProjection
		closeFn     = func() error { return nil }
		pingFn      = func(context.Context) error { return nil }
	)
	if dsn == "" {
		return nil, errors.New("TASK_DATABASE_DSN is required")
	}
	if paymentClient == nil {
		return nil, errors.New("PaymentRPC is required when the task service uses MySQL")
	}
	if discoveryClient == nil {
		return nil, errors.New("DiscoveryRPC is required when the task service uses MySQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := platformmysql.MigrateService(ctx, db, "task"); err != nil {
		cancel()
		_ = db.Close()
		return nil, err
	}
	cancel()
	mysqlStore := taskmysql.New(db)
	store, full = mysqlStore, mysqlStore
	accounts = nil
	comments = taskmysql.RiderComments{Store: mysqlStore}
	outboxStore = taskmysql.OutboxStore{Store: mysqlStore}
	likeCounts, _ = full.(repository.LikeProjection)
	reactionAdapter := rpcadapt.Reactions{Client: reactionClient}
	closeFn = db.Close
	pingFn = db.Ping

	taskService := taskservice.NewTaskService(store)
	if outboxStore != nil {
		taskService.WithOutbox(outboxStore)
	}
	// Cross-service reads go through RPC adapters; Task never reads a
	// neighbour's tables and never reimplements trusted-place validation.
	trusted := discoveryrpc.Trusted{Client: discoveryClient}
	taskService.WithTrustedPlaces(trusted, maps.GCJ02ToWGS84)
	taskService.WithPaymentGate(paymentrpc.Gate{Client: paymentClient})
	taskService.WithSettlements(paymentrpc.Settlements{Client: paymentClient})

	// Rider performance profiles and rider feedback are owned by Task.
	var riders *riderservice.RiderService
	var feedback *feedbackservice.RiderFeedbackService
	commentSvc := commentservice.NewCommentService(comments, true)
	commentSvc.AnonymousLabel = "匿名用户" // matches the existing rider public projection
	commentSvc.ReactionTarget = contract.TargetRiderComment
	commentSvc.Reactions = reactionAdapter
	var riderRepoVar repository.RiderRepository
	if accounts == nil && identityClient != nil {
		accounts = identityAccounts{client: identityClient}
	}
	if riderRepo, ok := full.(repository.RiderRepository); ok {
		riderRepoVar = riderRepo
		riders = riderservice.NewRiderService(riderRepo, accounts, taskService)
		if trackingClient != nil {
			riders.WithPositions(rpcadapt.Positions{Client: trackingClient})
		}
		if feedbackRepo, ok := full.(repository.RiderFeedback); ok {
			feedback = feedbackservice.NewRiderFeedbackService(feedbackRepo, store, riderRepo, accounts)
			feedback.ReactionTarget = contract.TargetRiderReview
			feedback.Reactions = reactionAdapter
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
	var relay *outboxservice.OutboxRelay
	var consumer *mq.Consumer
	var likeConsumer *mq.Consumer
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL != "" && outboxStore != nil {
		publisher = mq.NewPublisher(mqURL)
		relay = outboxservice.NewOutboxRelay(outboxStore, publisher, 2*time.Second, 50)
		consumer = mq.NewConsumer(mqURL, "task.rider-events", "rider.approved.v1")
		likeConsumer = mq.NewConsumerWithExchange(mqURL, "like.record.topic", "task.like-count",
			"RIDER_REVIEW.times.changed", "RIDER_COMMENT.times.changed")
		previousClose := closeFn
		closeFn = func() error {
			err := previousClose()
			if pubErr := publisher.Close(); err == nil {
				err = pubErr
			}
			if consumer != nil {
				_ = consumer.Close()
			}
			if likeConsumer != nil {
				_ = likeConsumer.Close()
			}
			return err
		}
	} else if outboxStore != nil {
		log.Printf("warning: RABBITMQ_URL is empty; task outbox will stay pending")
	}

	jobConfig := c.XXLJob.WithEnv()
	if jobConfig.Enabled {
		if discoveryClient == nil {
			return nil, errors.New("DiscoveryRPC is required when XXL-JOB is enabled")
		}
		if strings.TrimSpace(internalJobToken) == "" {
			return nil, errors.New("CITYFLOW_INTERNAL_JOB_TOKEN is required when XXL-JOB is enabled")
		}
	}
	jobRuntime, err := xxljob.NewRuntime(jobConfig, nil)
	if err != nil {
		return nil, err
	}

	return &ServiceContext{
		Config:           c,
		Tasks:            taskService,
		Riders:           riders,
		Comments:         commentSvc,
		Feedback:         feedback,
		RiderRepo:        riderRepoVar,
		Reactions:        reactionAdapter,
		LikeCounts:       likeCounts,
		Accounts:         accounts,
		Identity:         identityClient,
		Discovery:        discoveryClient,
		Secret:           []byte(secret),
		Status:           statusCache,
		Relay:            relay,
		Consumer:         consumer,
		LikeConsumer:     likeConsumer,
		XXLJob:           jobRuntime,
		InternalJobToken: internalJobToken,
		publisher:        publisher,
		closeFn:          closeFn,
		pingFn:           pingFn,
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
	s.XXLJob.Stop()
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

// StartJobs registers the Task-owned handlers and starts the embedded XXL-JOB
// executor. A nil runtime means scheduling is disabled and is not an error.
func (s *ServiceContext) StartJobs(ctx context.Context) error {
	if s == nil || s.XXLJob == nil {
		return nil
	}
	if s.Discovery == nil {
		return errors.New("DiscoveryRPC is required for rank.build")
	}
	builder := discoveryrpc.RankBuilder{
		Client: s.Discovery,
		Token:  s.InternalJobToken,
		Limit:  s.Config.XXLJobRankLimit,
	}
	return s.XXLJob.Start(ctx, xxljob.Registration{
		Name: "rank.build",
		Handler: func(ctx context.Context, _ *xxljob.RunReq) (string, error) {
			return builder.Build(ctx)
		},
	})
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
