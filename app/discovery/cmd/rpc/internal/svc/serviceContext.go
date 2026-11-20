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

	"cityflow/app/discovery/cmd/rpc/internal/config"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/application/repository"
	"cityflow/internal/application/service"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/reaction"
	"cityflow/internal/domain/user"
	"cityflow/internal/infrastructure/cache"
	"cityflow/internal/infrastructure/maps"
	"cityflow/internal/infrastructure/memory"
	"cityflow/internal/infrastructure/mq"
	mysqlstore "cityflow/internal/infrastructure/mysql"
	"cityflow/internal/platform/rpcadapt"
	"cityflow/internal/platform/rpcctx"

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

	MerchantSvc *service.MerchantService
	ReviewSvc   *service.MerchantReviewService
	CommentSvc  *service.CommentService
	Trusted     service.TrustedPlaces
	Map         *maps.TrustedSearch
	MapCache    *cache.MapCache
	MapPublic   MapPublic
	UploadDir   string

	Identity identityclient.Identity
	Task     taskclient.Task
	Reaction reactionclient.Reaction
	Secret   []byte
	Consumer *mq.Consumer
	Likes    repository.PlaceReviewLikeProjection

	closeFn func() error
	pingFn  func(context.Context) error
}

type accountPort struct {
	client identityclient.Identity
}

func (a accountPort) GetUser(ctx context.Context, id string) (*user.User, error) {
	if a.client == nil {
		return nil, domain.ErrUnavailable
	}
	return rpcadapt.Accounts{Client: a.client}.GetUser(ctx, id)
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

	var identity identityclient.Identity
	if len(c.IdentityRPC.Endpoints) > 0 {
		identity = identityclient.NewIdentity(zrpc.MustNewClient(c.IdentityRPC))
	}
	var task taskclient.Task
	if len(c.TaskRPC.Endpoints) > 0 {
		task = taskclient.NewTask(zrpc.MustNewClient(c.TaskRPC))
	}
	var reactions reactionclient.Reaction
	if len(c.ReactionRPC.Endpoints) > 0 {
		reactions = reactionclient.NewReaction(zrpc.MustNewClient(c.ReactionRPC))
	}

	var consumer *mq.Consumer
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	if mqURL != "" {
		consumer = mq.NewConsumer(mqURL, "discovery.like-projection", "like.times.PLACE_REVIEW")
	}

	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DISCOVERY_DATABASE_DSN"))
	}

	var (
		merchants   repository.MerchantRepository
		reviews     repository.MerchantReviews
		reviewAdmin repository.PlaceReviewAdmin
		media       repository.MerchantMedia
		places      repository.TrustedPlaces
		comments    repository.ReviewComments
		likes       repository.PlaceReviewLikeProjection
		accounts    accountPort
		closeFn     = func() error { return nil }
		pingFn      = func(context.Context) error { return nil }
	)
	if dsn == "" {
		memoryStore, err := memory.NewStore()
		if err != nil {
			return nil, err
		}
		merchants, reviews, media = memoryStore, memoryStore, memoryStore
		reviewAdmin, _ = reviews.(repository.PlaceReviewAdmin)
		comments = memory.PlaceComments{Store: memoryStore}
		likes = memoryStore
		closeFn = memoryStore.Close
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		mysqlStore, err := mysqlstore.Open(ctx, dsn)
		if err != nil {
			cancel()
			return nil, err
		}
		if err := mysqlStore.MigrateService(ctx, "discovery"); err != nil {
			cancel()
			mysqlStore.Close()
			return nil, err
		}
		cancel()
		merchants, reviews, media, places = mysqlStore, mysqlStore, mysqlStore, mysqlStore
		reviewAdmin, _ = reviews.(repository.PlaceReviewAdmin)
		comments = mysqlstore.PlaceComments{Store: mysqlStore}
		likes = mysqlStore
		closeFn = func() error { mysqlStore.Close(); return nil }
		pingFn = mysqlStore.Ping
	}
	accounts = accountPort{client: identity}

	trustedTTL := c.TrustedTTL
	if trustedTTL <= 0 {
		trustedTTL = 1800
	}
	if trustedTTL > 604800 {
		return nil, errors.New("TrustedTTL must be at most 604800 seconds")
	}
	trusted := service.TrustedPlaces{Store: places, Lifetime: time.Duration(trustedTTL) * time.Second}
	merchantSvc := service.NewMerchantService(merchants, accounts)
	reviewSvc := &service.MerchantReviewService{Merchants: merchants, Reviews: reviews, ReviewAdmin: reviewAdmin, Places: places, Media: media}
	if reactions != nil {
		reviewSvc.Reactions = rpcadapt.ReactionReader{Client: reactions}
	}
	commentSvc := service.NewCommentService(comments, false)
	commentSvc.ReactionTarget = reaction.TargetPlaceComment
	if reactions != nil {
		commentSvc.Reactions = rpcadapt.ReactionReader{Client: reactions}
	}
	if len(c.TaskRPC.Endpoints) > 0 {
		reviewSvc.Tasks = rpcadapt.Tasks{Client: task}
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
	var mapService *maps.TrustedSearch
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
		mapService = &maps.TrustedSearch{GuardedAmap: provider, Places: trusted, Adcode: settings.DefaultAdcode}
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

	return &ServiceContext{
		Config:      c,
		Merchants:   merchants,
		Reviews:     reviews,
		Media:       media,
		Places:      places,
		MerchantSvc: merchantSvc,
		ReviewSvc:   reviewSvc,
		CommentSvc:  commentSvc,
		Trusted:     trusted,
		Map:         mapService,
		MapCache:    mapCache,
		MapPublic:   public,
		UploadDir:   absoluteUpload,
		Identity:    identity,
		Task:        task,
		Reaction:    reactions,
		Secret:      []byte(secret),
		Consumer:    consumer,
		Likes:       likes,
		closeFn:     closeFn,
		pingFn:      pingFn,
	}, nil
}

func (s *ServiceContext) Close() error {
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
