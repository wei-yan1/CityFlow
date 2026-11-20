package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/task"
)

type MerchantReviewService struct {
	Merchants   repository.MerchantRepository
	Reviews     repository.MerchantReviews
	ReviewAdmin repository.PlaceReviewAdmin
	Places      repository.TrustedPlaces
	Media       repository.MerchantMedia
	Tasks       taskReader
	Reactions   ReactionReader
}

// ReactionState is the read-only projection used by review DTOs. Count is the
// absolute like count; Active means the requesting user currently likes it.
type ReactionState struct {
	Count  int64
	Active bool
}

// ReactionReader is implemented by the Reaction RPC adapter. Discovery never
// writes reaction records; it only reads the projection for review DTOs.
type ReactionReader interface {
	ReactionStates(ctx context.Context, targetType string, ids []string, kind, userID string) (map[string]ReactionState, error)
}

// PublicReview is the only shape that may reach an untrusted browser. It
// deliberately has no user_id, order_id or exact timestamp.
type PublicReview struct {
	ID          string `json:"id"`
	AuthorLabel string `json:"author_label"`
	Anonymous   bool   `json:"anonymous"`
	Rating      int    `json:"rating"`
	Comment     string `json:"comment"`
	Date        string `json:"date"`
	Likes       int    `json:"likes"`
	Liked       bool   `json:"liked"`
	Mine        bool   `json:"mine"`
}

// MerchantReview is the projection a merchant may see. It still has no
// identity or order linkage, only a stable/random label.
type MerchantReview struct {
	ID          string `json:"id"`
	AuthorLabel string `json:"author_label"`
	Anonymous   bool   `json:"anonymous"`
	Rating      int    `json:"rating"`
	Comment     string `json:"comment"`
	Date        string `json:"date"`
	Likes       int    `json:"likes"`
}

// AdminReview is the default redacted admin list. Identity remains a masked
// user_id until an explicit, audited reveal.
type AdminReview struct {
	ID           string `json:"id"`
	AuthorLabel  string `json:"author_label"`
	Anonymous    bool   `json:"anonymous"`
	UserIDMasked string `json:"user_id_masked"`
	OrderID      string `json:"order_id"`
	Rating       int    `json:"rating"`
	Comment      string `json:"comment"`
	CreatedAt    string `json:"created_at"`
}

type PublicPlace struct {
	ID             string         `json:"id"`
	Source         string         `json:"source"`
	Name           string         `json:"name"`
	Address        string         `json:"address"`
	Description    string         `json:"description"`
	BusinessStatus string         `json:"business_status"`
	AMAPPOIID      string         `json:"amap_poi_id,omitempty"`
	Rating         float64        `json:"rating"`
	ReviewCount    int            `json:"review_count"`
	Reviews        []PublicReview `json:"reviews"`
	Reviewed       bool           `json:"reviewed,omitempty"`
	AvatarURL      string         `json:"avatar_url,omitempty"`
	ImageURLs      []string       `json:"image_urls,omitempty"`
}

func (s MerchantReviewService) place(ctx context.Context, source, id string) (*PublicPlace, error) {
	if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return nil, fmt.Errorf("%w: 地点无效", domain.ErrInvalidRequest)
	}
	if source == "merchant" {
		p, e := s.Merchants.GetMerchant(ctx, id)
		if e != nil {
			return nil, e
		}
		if p.BusinessStatus != "OPEN" {
			return nil, domain.ErrNotFound
		}
		return s.placeFromMerchant(ctx, p)
	}
	if source == "amap" {
		places, e := s.Places.FindTrustedPlaces(ctx, []string{id}, time.Now().UTC())
		if e != nil {
			return nil, e
		}
		p, ok := places[id]
		if !ok {
			return nil, domain.ErrNotFound
		}
		if profile, e := s.Merchants.FindMerchantByAMapPOI(ctx, id); e == nil {
			return s.placeFromMerchant(ctx, profile)
		} else if !errors.Is(e, domain.ErrNotFound) {
			return nil, e
		}
		if profile, e := s.Merchants.FindMerchantNearName(ctx, p.Place.Name, p.Place.Location, 150); e == nil {
			return s.placeFromMerchant(ctx, profile)
		} else if !errors.Is(e, domain.ErrNotFound) {
			return nil, e
		}
		return &PublicPlace{ID: id, Source: source, Name: p.Place.Name, Address: p.Place.Address}, nil
	}
	return nil, fmt.Errorf("%w: 地点来源无效", domain.ErrInvalidRequest)
}

func (s MerchantReviewService) placeFromMerchant(ctx context.Context, p *merchant.Profile) (*PublicPlace, error) {
	place := &PublicPlace{ID: p.ID, Source: "merchant", Name: p.Name, Address: p.Address, Description: p.Description, BusinessStatus: p.BusinessStatus, AMAPPOIID: p.AMAPPOIID}
	if s.Media != nil {
		avatar, images, e := s.Media.GetMerchantMedia(ctx, p.ID)
		if e != nil {
			return nil, e
		}
		place.AvatarURL = avatar
		place.ImageURLs = images
	}
	return place, nil
}

func (s MerchantReviewService) SaveMedia(ctx context.Context, userID, avatar string, images []string) error {
	if s.Media == nil {
		return domain.ErrUnavailable
	}
	profile, err := s.Merchants.MerchantForUser(ctx, userID)
	if err != nil {
		return err
	}
	if len(images) > 6 {
		return fmt.Errorf("%w: 最多添加六张图片", domain.ErrInvalidRequest)
	}
	valid := func(raw string) bool {
		if strings.HasPrefix(raw, "/uploads/") {
			return len(raw) <= 2048 && !strings.Contains(raw, "..") && !strings.ContainsAny(raw, "?#\r\n")
		}
		u, e := url.Parse(raw)
		return e == nil && len(raw) <= 2048 && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
	}
	avatar = strings.TrimSpace(avatar)
	if avatar != "" && !valid(avatar) {
		return fmt.Errorf("%w: 头像链接无效", domain.ErrInvalidRequest)
	}
	out := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if !valid(image) {
			return fmt.Errorf("%w: 图片链接无效", domain.ErrInvalidRequest)
		}
		out = append(out, image)
	}
	return s.Media.SaveMerchantMedia(ctx, profile.ID, avatar, out)
}

func (s MerchantReviewService) GetMedia(ctx context.Context, userID string) (string, []string, error) {
	if s.Media == nil {
		return "", nil, domain.ErrUnavailable
	}
	profile, err := s.Merchants.MerchantForUser(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	return s.Media.GetMerchantMedia(ctx, profile.ID)
}
func (s MerchantReviewService) Detail(ctx context.Context, source, id, userID string, offset int) (*PublicPlace, error) {
	if offset < 0 || offset > 100000 {
		return nil, fmt.Errorf("%w: 页码无效", domain.ErrInvalidRequest)
	}
	p, e := s.place(ctx, source, id)
	if e != nil {
		return nil, e
	}
	reviews, count, rating, e := s.mergedReviews(ctx, p, userID, offset)
	if e != nil {
		return nil, e
	}
	public := make([]PublicReview, 0, len(reviews))
	for _, review := range reviews {
		public = append(public, s.publicReview(review))
	}
	p.Reviews = public
	p.ReviewCount = count
	p.Rating = rating
	return p, nil
}

// mergedReviews folds reviews stored against the platform store and its
// bound AMap POI into one public profile. Platform reviews are canonical.
func (s MerchantReviewService) mergedReviews(ctx context.Context, p *PublicPlace, userID string, offset int) ([]merchant.Review, int, float64, error) {
	reviews, count, rating, e := s.placeReviews(ctx, p.Source, p.ID, userID, 20, offset)
	if e != nil {
		return nil, 0, 0, e
	}
	s.overlayLikes(ctx, reviews, userID)
	if p.Source != "merchant" || p.AMAPPOIID == "" {
		return reviews, count, rating, nil
	}
	other, otherCount, otherRating, e := s.placeReviews(ctx, "amap", p.AMAPPOIID, userID, 20, 0)
	if e != nil {
		return nil, 0, 0, e
	}
	s.overlayLikes(ctx, other, userID)
	if otherCount == 0 {
		return reviews, count, rating, nil
	}
	seen := make(map[string]bool, len(reviews)+len(other))
	all := make([]merchant.Review, 0, len(reviews)+len(other))
	for _, review := range reviews {
		if !seen[review.ID] {
			seen[review.ID] = true
			all = append(all, review)
		}
	}
	for _, review := range other {
		if !seen[review.ID] {
			seen[review.ID] = true
			all = append(all, review)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	totalCount := count + otherCount
	totalRating := rating*float64(count) + otherRating*float64(otherCount)
	if totalCount > 0 {
		totalRating /= float64(totalCount)
	}
	if offset >= len(all) {
		return []merchant.Review{}, totalCount, totalRating, nil
	}
	end := offset + 20
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], totalCount, totalRating, nil
}
func (s MerchantReviewService) Create(ctx context.Context, source, id, userID string, req merchant.ReviewRequest) error {
	req.Comment = strings.TrimSpace(req.Comment)
	req.OrderID = strings.TrimSpace(req.OrderID)
	if req.Rating < 1 || req.Rating > 5 || !utf8.ValidString(req.Comment) || utf8.RuneCountInString(req.Comment) > 500 {
		return fmt.Errorf("%w: 评分需为 1 至 5 星，评价最多 500 字", domain.ErrInvalidRequest)
	}
	if req.OrderID == "" {
		return fmt.Errorf("%w: 评价必须关联一笔已完成的订单", domain.ErrInvalidRequest)
	}
	place, e := s.place(ctx, source, id)
	if e != nil {
		return e
	}
	if s.Tasks == nil {
		return domain.ErrUnavailable
	}
	order, e := s.Tasks.Get(ctx, req.OrderID)
	if e != nil {
		if errors.Is(e, domain.ErrNotFound) {
			return fmt.Errorf("%w: 关联订单不存在", domain.ErrInvalidRequest)
		}
		return e
	}
	if order.CreatedBy != userID || order.Status != task.StatusCompleted {
		return fmt.Errorf("%w: 只能评价自己已完成订单对应的商家", domain.ErrForbidden)
	}
	if !orderMatchesPlace(order, source, id, *place) {
		return fmt.Errorf("%w: 该订单与当前地点不匹配", domain.ErrInvalidRequest)
	}
	if existing, _, _, e := s.Reviews.PlaceReviews(ctx, place.Source, place.ID, userID, 100, 0); e == nil {
		for _, review := range existing {
			if review.OrderID == req.OrderID && review.UserID == userID {
				return fmt.Errorf("%w: 该订单已评价", domain.ErrConflict)
			}
		}
	}
	var key [16]byte
	if _, e := rand.Read(key[:]); e != nil {
		return e
	}
	return s.Reviews.CreatePlaceReview(ctx, merchant.Review{
		ID: hex.EncodeToString(key[:]), OrderID: req.OrderID, PlaceSource: place.Source, PlaceID: place.ID,
		UserID: userID, Anonymous: req.Anonymous, AuthorLabel: reviewAuthorLabel(req.Anonymous, userID),
		Rating: req.Rating, Comment: req.Comment,
	})
}

func (s MerchantReviewService) CreateForTask(ctx context.Context, taskID, userID string, req merchant.ReviewRequest) (*PublicReview, error) {
	if s.Tasks == nil {
		return nil, domain.ErrUnavailable
	}
	order, e := s.Tasks.Get(ctx, taskID)
	if e != nil {
		return nil, e
	}
	if order.CreatedBy != userID {
		return nil, domain.ErrForbidden
	}
	source, id, e := s.reviewTarget(ctx, order)
	if e != nil {
		return nil, e
	}
	req.OrderID = taskID
	if e := s.Create(ctx, source, id, userID, req); e != nil {
		return nil, e
	}
	canonical, e := s.place(ctx, source, id)
	if e != nil {
		return nil, e
	}
	reviews, _, _, e := s.Reviews.PlaceReviews(ctx, canonical.Source, canonical.ID, userID, 20, 0)
	if e != nil {
		return nil, e
	}
	for _, review := range reviews {
		if review.OrderID == taskID && review.UserID == userID {
			public := s.publicReview(review)
			return &public, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s MerchantReviewService) ReviewTargetForTask(ctx context.Context, taskID, userID string) (*PublicPlace, error) {
	if s.Tasks == nil {
		return nil, domain.ErrUnavailable
	}
	order, e := s.Tasks.Get(ctx, taskID)
	if e != nil {
		return nil, e
	}
	if order.CreatedBy != userID {
		return nil, domain.ErrForbidden
	}
	source, id, e := s.reviewTarget(ctx, order)
	if e != nil {
		return nil, e
	}
	place, e := s.place(ctx, source, id)
	if e != nil {
		return nil, e
	}
	reviews, _, _, e := s.Reviews.PlaceReviews(ctx, place.Source, place.ID, userID, 100, 0)
	if e != nil {
		return nil, e
	}
	for _, review := range reviews {
		if review.OrderID == taskID && review.UserID == userID {
			place.Reviewed = true
			break
		}
	}
	return place, nil
}

func (s MerchantReviewService) reviewTarget(ctx context.Context, order *task.Task) (string, string, error) {
	if poi := strings.TrimSpace(order.DestinationPOIID); poi != "" {
		if s.Merchants != nil {
			if profile, e := s.Merchants.GetMerchant(ctx, poi); e == nil {
				return "merchant", profile.ID, nil
			} else if !errors.Is(e, domain.ErrNotFound) {
				return "", "", e
			}
		}
		return "amap", poi, nil
	}
	name := strings.TrimSpace(strings.Split(order.Destination, " · ")[0])
	point := location.Point{}
	if order.DestinationLocation != nil {
		point = *order.DestinationLocation
	}
	if name != "" && point.Valid() && s.Merchants != nil {
		if profile, e := s.Merchants.FindMerchantNearName(ctx, name, point, 200); e == nil {
			return "merchant", profile.ID, nil
		}
	}
	return "", "", fmt.Errorf("%w: 该订单没有可评价的商家地点", domain.ErrInvalidRequest)
}

func orderMatchesPlace(order *task.Task, source, id string, place PublicPlace) bool {
	if order.DestinationPOIID != "" && source == "amap" && order.DestinationPOIID == id {
		return true
	}
	if strings.TrimSpace(order.Destination) != "" && strings.Contains(order.Destination, place.Name) {
		return true
	}
	if source == "merchant" && order.DestinationPOIID != "" && place.Source == "merchant" && place.ID == id {
		return true
	}
	return false
}

func (s MerchantReviewService) placeReviews(ctx context.Context, source, id, userID string, limit, offset int) ([]merchant.Review, int, float64, error) {
	if reader, ok := s.Reviews.(repository.PublicPlaceReviewReader); ok {
		return reader.PublicPlaceReviews(ctx, source, id, userID, limit, offset)
	}
	return s.Reviews.PlaceReviews(ctx, source, id, userID, limit, offset)
}

func (s MerchantReviewService) publicReview(review merchant.Review) PublicReview {
	label := review.AuthorLabel
	if strings.TrimSpace(label) == "" {
		label = fallbackReviewLabel(review.Anonymous, review.UserID)
	}
	return PublicReview{
		ID: review.ID, AuthorLabel: label, Anonymous: review.Anonymous,
		Rating: review.Rating, Comment: review.Comment, Date: reviewDate(review.CreatedAt),
		Likes: review.Likes, Liked: review.Liked, Mine: review.Mine,
	}
}

func (s MerchantReviewService) merchantReview(review merchant.Review) MerchantReview {
	label := review.AuthorLabel
	if strings.TrimSpace(label) == "" {
		label = fallbackReviewLabel(review.Anonymous, review.UserID)
	}
	return MerchantReview{
		ID: review.ID, AuthorLabel: label, Anonymous: review.Anonymous,
		Rating: review.Rating, Comment: review.Comment, Date: reviewDate(review.CreatedAt), Likes: review.Likes,
	}
}

func (s MerchantReviewService) MerchantReviews(ctx context.Context, userID string, limit, offset int) ([]MerchantReview, error) {
	if s.Merchants == nil {
		return nil, domain.ErrUnavailable
	}
	profile, err := s.Merchants.MerchantForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	reviews, _, _, err := s.placeReviews(ctx, "merchant", profile.ID, "", limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]MerchantReview, 0, len(reviews))
	for _, review := range reviews {
		out = append(out, s.merchantReview(review))
	}
	return out, nil
}

func (s MerchantReviewService) ListAdminReviews(ctx context.Context, source, placeID string, limit, offset int) ([]AdminReview, int, error) {
	if s.ReviewAdmin == nil {
		return nil, 0, domain.ErrUnavailable
	}
	reviews, total, err := s.ReviewAdmin.ListPlaceReviewsAdmin(ctx, source, placeID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AdminReview, 0, len(reviews))
	for _, review := range reviews {
		label := review.AuthorLabel
		if strings.TrimSpace(label) == "" {
			label = fallbackReviewLabel(review.Anonymous, review.UserID)
		}
		out = append(out, AdminReview{
			ID: review.ID, AuthorLabel: label, Anonymous: review.Anonymous,
			UserIDMasked: maskUserID(review.UserID), OrderID: review.OrderID,
			Rating: review.Rating, Comment: review.Comment,
			CreatedAt: review.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, total, nil
}

func (s MerchantReviewService) RevealIdentity(ctx context.Context, reviewID, adminID, reason string) (string, error) {
	if s.ReviewAdmin == nil {
		return "", domain.ErrUnavailable
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 10 {
		return "", fmt.Errorf("%w: reveal理由至少需要 10 个字", domain.ErrInvalidRequest)
	}
	if len([]rune(reason)) > 500 {
		return "", fmt.Errorf("%w: reveal理由不能超过 500 字", domain.ErrInvalidRequest)
	}
	return s.ReviewAdmin.RevealPlaceReviewIdentity(ctx, reviewID, adminID, reason)
}

func reviewAuthorLabel(anonymous bool, userID string) string {
	if anonymous {
		return "匿名用户·" + randomReviewSuffix()
	}
	return "用户·" + stableReviewSuffix(userID)
}

func fallbackReviewLabel(anonymous bool, userID string) string {
	if anonymous {
		return "匿名用户"
	}
	return "用户·" + stableReviewSuffix(userID)
}

func stableReviewSuffix(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:])[:6]
}

func randomReviewSuffix() string {
	var buffer [3]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("%06x", time.Now().UTC().UnixNano()&0xffffff)
	}
	return hex.EncodeToString(buffer[:])
}

func reviewDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
}

func maskUserID(userID string) string {
	if len(userID) <= 8 {
		return "****"
	}
	return userID[:4] + "****" + userID[len(userID)-4:]
}
func (s MerchantReviewService) Delete(ctx context.Context, id, userID string) error {
	return s.Reviews.DeletePlaceReview(ctx, id, userID)
}

func (s MerchantReviewService) overlayLikes(ctx context.Context, reviews []merchant.Review, userID string) {
	if s.Reactions == nil || len(reviews) == 0 {
		return
	}
	ids := make([]string, 0, len(reviews))
	for i := range reviews {
		ids = append(ids, reviews[i].ID)
	}
	states, err := s.Reactions.ReactionStates(ctx, "PLACE_REVIEW", ids, "LIKE", userID)
	if err != nil {
		log.Printf("warning: read reaction projection: %v", err)
		return
	}
	for i := range reviews {
		state, ok := states[reviews[i].ID]
		if !ok {
			continue
		}
		reviews[i].Likes = int(state.Count)
		reviews[i].Liked = state.Active
	}
}
