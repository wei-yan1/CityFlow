package svc

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	merchantservice "cityflow/app/discovery/application/merchant"
	reviewservice "cityflow/app/discovery/application/review"
	trustedservice "cityflow/app/discovery/application/trusted"
	"cityflow/app/discovery/cmd/rpc/internal/config"
	discoverymaps "cityflow/app/discovery/internal/adapter/maps"
	discoverymysql "cityflow/app/discovery/internal/adapter/mysql"
	discoveryredis "cityflow/app/discovery/internal/adapter/redis"
	"cityflow/app/discovery/repository"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/common/contract"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/integration/rpcadapt"
	"cityflow/internal/platform/cache"
	"cityflow/internal/platform/maps"
	"cityflow/internal/platform/mq"
	platformmysql "cityflow/internal/platform/mysql"
	"cityflow/internal/platform/rpcctx"
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MapPublic struct {
	CityConfigured   bool           `json:"city_configured"`
	SearchConfigured bool           `json:"search_configured"`
	RidingConfigured bool           `json:"riding_configured"`
	CityEnabled      bool           `json:"city_enabled"`
	DefaultCity      string         `json:"default_city"`
	DefaultAdcode    string         `json:"default_adcode"`
	DefaultCenter    location.Point `json:"default_center"`
	Provider         string         `json:"provider"`
	AmapJSKey        string         `json:"amap_js_key,omitempty"`
	AmapSecurityCode string         `json:"amap_security_code,omitempty"`
	SearchEnabled    bool           `json:"search_enabled"`
	RidingEnabled    bool           `json:"riding_enabled"`
	TileURL          string         `json:"tile_url"`
	Attribution      string         `json:"attribution"`
}

type ServiceContext struct {
	Config    config.Config
	Merchants repository.MerchantRepository
	Reviews   repository.MerchantReviews
	Media     repository.MerchantMedia
	Places    repository.TrustedPlaces

	MerchantSvc *merchantservice.MerchantService
	LikeSvc     *merchantservice.MerchantLikeService
	ReviewSvc   *reviewservice.MerchantReviewService
	CommentSvc  *reviewservice.CommentService
	Trusted     *trustedservice.Service
	Map         *discoverymaps.TrustedSearch
	MapCache    *cache.MapCache
	MapPublic   MapPublic
	UploadDir   string

	Identity  identityclient.Identity
	Task      taskclient.Task
	Reaction  reactionclient.Reaction
	Reactions rpcadapt.Reactions
	Secret    []byte
	Consumer  *mq.Consumer
	Likes     repository.LikeProjection
	// InternalJobToken authenticates scheduler-only RPCs such as rank rebuild.
	InternalJobToken string
	XXLJob           *xxljob.Runtime

	closeFn func() error
	pingFn  func(context.Context) error
}

type accountPort struct {
	client identityclient.Identity
}

func (a accountPort) GetAccount(ctx context.Context, id string) (*merchantservice.Account, error) {
	if a.client == nil {
		return nil, domain.ErrUnavailable
	}
	account, err := rpcadapt.Accounts{Client: a.client}.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	return &merchantservice.Account{ID: account.ID, Role: account.Role, Status: account.Status}, nil
}

// taskViews adapts Task's RPC projection to the local TaskView used by the
// review application service.
type taskViews struct{ tasks rpcadapt.Tasks }

func (a taskViews) Get(ctx context.Context, id string) (*reviewservice.TaskView, error) {
	item, err := a.tasks.Snapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	return &reviewservice.TaskView{
		ID: item.ID, CreatedBy: item.CreatedBy, Status: item.Status,
		Destination: item.Destination, DestinationPOIID: item.DestinationPOIID,
		DestinationLocation: item.DestinationLocation,
	}, nil
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
	if internalJobToken == "" {
		log.Printf("warning: internal job token is not set; rank rebuild RPC is disabled")
	}

	var identity identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identity = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}
	var task taskclient.Task
	if len(c.TaskRPC.Endpoints) > 0 {
		task = taskclient.NewTask(zrpc.MustNewClient(c.TaskRPC))
	}
	var reaction reactionclient.Reaction
	if len(c.ReactionRPC.Endpoints) > 0 {
		reaction = reactionclient.NewReaction(zrpc.MustNewClient(c.ReactionRPC))
	}
	var consumer *mq.Consumer
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL != "" {
		consumer = mq.NewConsumerWithExchange(mqURL, "like.record.topic", "discovery.like-count",
			"PLACE_REVIEW.times.changed", "PLACE_COMMENT.times.changed")
	}

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DISCOVERY_DATABASE_DSN"))
	}

	var (
		merchants     repository.MerchantRepository
		reviews       repository.MerchantReviews
		reviewAdmin   repository.PlaceReviewAdmin
		media         repository.MerchantMedia
		places        repository.TrustedPlaces
		comments      repository.ReviewComments
		likes         repository.LikeProjection
		merchantLikes repository.MerchantLikes
		accounts      accountPort
		closeFn       = func() error { return nil }
		pingFn        = func(context.Context) error { return nil }
	)
	if dsn == "" {
		return nil, errors.New("DISCOVERY_DATABASE_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := platformmysql.MigrateService(ctx, db, "discovery"); err != nil {
		cancel()
		_ = db.Close()
		return nil, err
	}
	cancel()
	store := discoverymysql.New(db)
	merchants, reviews, media, places = store, store, store, store
	reviewAdmin, _ = reviews.(repository.PlaceReviewAdmin)
	comments = discoverymysql.PlaceComments{Store: store}
	likes = store
	merchantLikes = store
	closeFn = db.Close
	pingFn = db.Ping
	accounts = accountPort{client: identity}

	trustedTTL := c.TrustedTTL
	if trustedTTL <= 0 {
		trustedTTL = 1800
	}
	if trustedTTL > 604800 {
		return nil, errors.New("TrustedTTL must be at most 604800 seconds")
	}
	trusted := trustedservice.New(places, time.Duration(trustedTTL)*time.Second)
	merchantSvc := merchantservice.NewMerchantService(merchants, accounts)
	reactionAdapter := rpcadapt.Reactions{Client: reaction}
	reviewSvc := &reviewservice.MerchantReviewService{Merchants: merchants, Reviews: reviews, ReviewAdmin: reviewAdmin, Places: places, Media: media}
	reviewSvc.Reactions = reactionAdapter
	commentSvc := reviewservice.NewCommentService(comments, false)
	commentSvc.ReactionTarget = contract.TargetPlaceComment
	commentSvc.Reactions = reactionAdapter
	if len(c.TaskRPC.Endpoints) > 0 {
		reviewSvc.Tasks = taskViews{tasks: rpcadapt.Tasks{Client: task}}
	}

	uploadDir := strings.TrimSpace(c.UploadDir)
	if uploadDir == "" {
		uploadDir = strings.TrimSpace(os.Getenv("CITYFLOW_UPLOAD_DIR"))
	}
	if uploadDir == "" {
		uploadDir = "uploads"
	}
	absoluteUpload, err := filepath.Abs(uploadDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absoluteUpload, 0o755); err != nil {
		return nil, err
	}

	settings, err := loadMapSettings(strings.TrimSpace(c.MapConfig))
	if err != nil {
		return nil, err
	}
	var mapCache *cache.MapCache
	var mapRedis *cache.Redis
	if address := strings.TrimSpace(os.Getenv("REDIS_ADDR")); address != "" {
		mapRedis = cache.NewRedis(address, os.Getenv("REDIS_PASSWORD"), positiveEnv("REDIS_DB", 0))
		mapCache = cache.NewMapCache(mapRedis, int64(positiveEnv("MAP_CACHE_MB", 128))<<20, positiveEnv("MAP_PROVIDER_REQUESTS_PER_MINUTE", 60))
	}
	if mapCache != nil {
		if err := mapRedis.Ping(context.Background()); err != nil {
			log.Printf("warning: Redis map cache unavailable: %v", err)
		}
	}
	var hotRanking repository.MerchantHotRanking
	if mapRedis != nil {
		hotStore := discoveryredis.NewHotStore(mapRedis.Client())
		hotRanking = hotStore
		merchantSvc.SetProfileCache(hotStore)
	}
	likeSvc := merchantservice.NewMerchantLikeService(merchantLikes, merchants, hotRanking, store)
	likeSvc.SetHotSnapshot(store)
	var mapService *discoverymaps.TrustedSearch
	if strings.TrimSpace(settings.AmapWebKey) != "" && mapCache != nil {
		provider := &maps.GuardedAmap{
			Provider:    maps.NewAmap(settings.AmapWebKey),
			Cache:       mapCache,
			PermitCache: true,
			DistrictTTL: time.Hour,
			SearchTTL:   time.Minute,
			CityTTL:     10 * time.Minute,
			RouteTTL:    time.Minute,
			HotCities:   hotCityAliases(settings.DefaultCity, settings.DefaultAdcode),
		}
		mapService = &discoverymaps.TrustedSearch{GuardedAmap: provider, Places: trusted, Adcode: settings.DefaultAdcode}
	} else if strings.TrimSpace(settings.AmapWebKey) == "" {
		log.Printf("warning: AMAP_WEB_SERVICE_KEY is not set; Discovery map endpoints will return 503")
	}
	public := MapPublic{
		CityConfigured:   mapService != nil,
		SearchConfigured: mapService != nil,
		RidingConfigured: mapService != nil,
		CityEnabled:      mapService != nil,
		DefaultCity:      settings.DefaultCity,
		DefaultAdcode:    settings.DefaultAdcode,
		DefaultCenter:    location.Point{Latitude: settings.CenterLatitude, Longitude: settings.CenterLongitude},
		Provider:         settings.Provider,
		AmapJSKey:        settings.AmapJSKey,
		AmapSecurityCode: settings.AmapSecurityCode,
		SearchEnabled:    mapService != nil,
		RidingEnabled:    mapService != nil,
		TileURL:          settings.TileURL,
		Attribution:      settings.Attribution,
	}

	originalClose := closeFn
	closeFn = func() error {
		if mapCache != nil {
			mapCache.Close()
		}
		if mapRedis != nil {
			_ = mapRedis.Close()
		}
		return originalClose()
	}

	jobRuntime, err := xxljob.NewRuntime(c.XXLJob.WithEnv("DISCOVERY"), nil)
	if err != nil {
		_ = closeFn()
		return nil, err
	}

	return &ServiceContext{
		Config:           c,
		Merchants:        merchants,
		Reviews:          reviews,
		Media:            media,
		Places:           places,
		MerchantSvc:      merchantSvc,
		LikeSvc:          likeSvc,
		ReviewSvc:        reviewSvc,
		CommentSvc:       commentSvc,
		Trusted:          trusted,
		Map:              mapService,
		MapCache:         mapCache,
		MapPublic:        public,
		UploadDir:        absoluteUpload,
		Identity:         identity,
		Task:             task,
		Reaction:         reaction,
		Reactions:        reactionAdapter,
		Secret:           []byte(secret),
		Consumer:         consumer,
		Likes:            likes,
		InternalJobToken: internalJobToken,
		XXLJob:           jobRuntime,
		closeFn:          closeFn,
		pingFn:           pingFn,
	}, nil
}

func (s *ServiceContext) Close() error {
	if s.XXLJob != nil {
		s.XXLJob.Stop()
	}
	if s.Consumer != nil {
		_ = s.Consumer.Close()
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

// AccountActive is the interceptor's authority check. The access token is
// already signature-verified; Identity confirms that the account still exists
// and is active.
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

func hotCityAliases(city, adcode string) map[string]bool {
	hot := make(map[string]bool, 3)
	city = strings.TrimSpace(city)
	adcode = strings.TrimSpace(adcode)
	if city != "" {
		hot[city] = true
		if suffixless := strings.TrimSuffix(city, "市"); suffixless != "" {
			hot[suffixless] = true
		}
	}
	if adcode != "" {
		hot[adcode] = true
	}
	return hot
}

func positiveEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
