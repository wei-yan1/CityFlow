package merchantservice

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/app/discovery/repository"
	"cityflow/internal/common/domain"
)

const (
	defaultHotLimit = 10
	maxHotLimit     = 50
	hotOverfetch    = 4
)

var cst = time.FixedZone("CST", 8*60*60)

type LikeState struct {
	Active   bool
	Count    int64
	Month    string
	Category string
}

// MyLikeStats is the signed-in user's own merchant-like total. It deliberately
// stays separate from the public ranking response so an anonymous visitor can
// still read the list.
type MyLikeStats struct {
	Month   string `json:"month"`
	Monthly int64  `json:"monthly_count"`
	Total   int64  `json:"total_count"`
}

type HotMerchantReview struct {
	ID          string `json:"id"`
	AuthorLabel string `json:"author_label"`
	Anonymous   bool   `json:"anonymous,omitempty"`
	Rating      int    `json:"rating"`
	Comment     string `json:"comment"`
	Likes       int    `json:"likes"`
	Date        string `json:"date"`
}

type HotMerchantItem struct {
	Rank            int                `json:"rank"`
	MerchantID      string             `json:"merchant_id"`
	Name            string             `json:"name"`
	Category        string             `json:"category"`
	LikeCount       int64              `json:"like_count"`
	Description     string             `json:"description,omitempty"`
	Address         string             `json:"address,omitempty"`
	AMAPPOIID       string             `json:"amap_poi_id,omitempty"`
	Latitude        float64            `json:"latitude"`
	Longitude       float64            `json:"longitude"`
	BusinessStatus  string             `json:"business_status"`
	ServiceTypes    []string           `json:"service_types"`
	CapacityPerHour int                `json:"capacity_per_hour"`
	AvatarURL       string             `json:"avatar_url,omitempty"`
	ReviewCount     int                `json:"review_count"`
	Rating          float64            `json:"rating"`
	TopReview       *HotMerchantReview `json:"top_review,omitempty"`
}

type HotMerchantList struct {
	Month    string            `json:"month"`
	Category string            `json:"category"`
	Items    []HotMerchantItem `json:"items"`
}

type MerchantLikeService struct {
	likes     repository.MerchantLikes
	profiles  repository.MerchantRepository
	hot       repository.MerchantHotRanking
	cards     repository.MerchantCardExtras
	snapshots repository.MerchantHotSnapshot
	now       func() time.Time
	// Per-merchant serialization keeps the absolute score written to Redis in
	// the same order as the MySQL count read. A multi-instance deployment still
	// needs a distributed lock or an outbox; the current Discovery deployment is
	// single-writer for this path.
	locks sync.Map
}

func NewMerchantLikeService(likes repository.MerchantLikes, profiles repository.MerchantRepository, hot repository.MerchantHotRanking, cards repository.MerchantCardExtras) *MerchantLikeService {
	return &MerchantLikeService{
		likes:    likes,
		profiles: profiles,
		hot:      hot,
		cards:    cards,
		now:      func() time.Time { return time.Now().In(cst) },
	}
}

// SetHotSnapshot wires the durable monthly Top N store. It is optional for
// tests and read-only callers, but Rebuild requires it.
func (s *MerchantLikeService) SetHotSnapshot(snapshots repository.MerchantHotSnapshot) {
	s.snapshots = snapshots
}

// HotRebuildResult is the scheduler-facing summary of one rebuild run.
type HotRebuildResult struct {
	Month      string         `json:"month"`
	Categories map[string]int `json:"categories"`
	Items      int            `json:"items"`
}

// MonthKey returns the month bucket used by both the fact table and the ZSET.
func MonthKey(now time.Time) string {
	return now.In(cst).Format("200601")
}

// Set adds or removes the current user's monthly like. MySQL is authoritative:
// Redis only receives the resulting absolute count, so a Redis failure never
// fails the like and the next write repairs the score.
func (s *MerchantLikeService) Set(ctx context.Context, merchantID, userID string, active bool) (LikeState, error) {
	merchantID = strings.TrimSpace(merchantID)
	userID = strings.TrimSpace(userID)
	if merchantID == "" || len(merchantID) > 128 {
		return LikeState{}, fmt.Errorf("%w: invalid merchant_id", domain.ErrInvalidRequest)
	}
	if userID == "" {
		return LikeState{}, domain.ErrUnauthorized
	}
	lock := s.merchantLock(merchantID)
	lock.Lock()
	defer lock.Unlock()
	profile, err := s.profiles.GetMerchant(ctx, merchantID)
	if err != nil {
		return LikeState{}, err
	}
	month := MonthKey(s.now())
	changed := false
	if active {
		changed, err = s.likes.AddMerchantLike(ctx, merchantID, userID, month)
	} else {
		changed, err = s.likes.RemoveMerchantLike(ctx, merchantID, userID, month)
	}
	if err != nil {
		return LikeState{}, fmt.Errorf("%w: merchant like store unavailable", domain.ErrUnavailable)
	}
	count, err := s.likes.MerchantLikeCount(ctx, merchantID, month)
	if err != nil {
		return LikeState{}, fmt.Errorf("%w: merchant like count unavailable", domain.ErrUnavailable)
	}
	if changed {
		s.project(ctx, month, profile.Category(), merchantID, count)
	}
	return LikeState{Active: active, Count: count, Month: month, Category: profile.Category()}, nil
}

// States returns absolute counts and the current user's monthly flags in one
// batched MySQL read. It never calls GetMerchant in a loop.
func (s *MerchantLikeService) States(ctx context.Context, merchantIDs []string, userID string) ([]LikeState, error) {
	ids := uniqueMerchantIDs(merchantIDs, maxHotLimit)
	if len(ids) == 0 {
		return []LikeState{}, nil
	}
	month := MonthKey(s.now())
	counts, err := s.likes.MerchantLikeCounts(ctx, month, ids)
	if err != nil {
		return nil, fmt.Errorf("%w: merchant like counts unavailable", domain.ErrUnavailable)
	}
	liked := map[string]bool{}
	if strings.TrimSpace(userID) != "" {
		liked, err = s.likes.MerchantLikeUsers(ctx, userID, month, ids)
		if err != nil {
			return nil, fmt.Errorf("%w: merchant like state unavailable", domain.ErrUnavailable)
		}
	}
	out := make([]LikeState, 0, len(ids))
	for _, id := range ids {
		out = append(out, LikeState{
			Active: liked[id],
			Count:  counts[id],
			Month:  month,
		})
	}
	return out, nil
}

func (s *MerchantLikeService) State(ctx context.Context, merchantID, userID string) (LikeState, error) {
	merchantID = strings.TrimSpace(merchantID)
	profile, err := s.profiles.GetMerchant(ctx, merchantID)
	if err != nil {
		return LikeState{}, err
	}
	states, err := s.States(ctx, []string{merchantID}, userID)
	if err != nil {
		return LikeState{}, err
	}
	if len(states) == 0 {
		return LikeState{}, fmt.Errorf("%w: merchant not found", domain.ErrNotFound)
	}
	state := states[0]
	state.Category = profile.Category()
	return state, nil
}

// MyStats returns the signed-in user's current-month and lifetime merchant
// like counts. One indexed MySQL range covers both numbers.
func (s *MerchantLikeService) MyStats(ctx context.Context, userID string) (MyLikeStats, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return MyLikeStats{}, domain.ErrUnauthorized
	}
	month := MonthKey(s.now())
	totals, err := s.likes.MerchantLikeStats(ctx, userID, month)
	if err != nil {
		return MyLikeStats{}, fmt.Errorf("%w: merchant like totals unavailable", domain.ErrUnavailable)
	}
	return MyLikeStats{Month: month, Monthly: totals.Monthly, Total: totals.Lifetime}, nil
}

// Hot returns the current month's Top N for one partition. Redis supplies the
// ids; MySQL supplies missing details and is the rebuild source when Redis is
// empty or has too few eligible candidates.
func (s *MerchantLikeService) Hot(ctx context.Context, category string, limit int) (HotMerchantList, error) {
	category = strings.ToUpper(strings.TrimSpace(category))
	if !merchant.ValidCategory(category) {
		return HotMerchantList{}, fmt.Errorf("%w: category must be FOOD, SUPPLIES or OTHER", domain.ErrInvalidRequest)
	}
	if limit <= 0 {
		limit = defaultHotLimit
	}
	if limit > maxHotLimit {
		limit = maxHotLimit
	}
	month := MonthKey(s.now())
	out := HotMerchantList{Month: month, Category: category, Items: []HotMerchantItem{}}
	if s.hot != nil {
		scores, err := s.hot.TopMerchantScores(ctx, month, category, limit*hotOverfetch)
		if err != nil {
			log.Printf("merchant hot top: %v", err)
		} else {
			items := s.hotItems(ctx, scores, category, limit)
			if len(items) >= limit {
				out.Items = items
				return out, nil
			}
			if len(items) > 0 {
				out.Items = items
			}
		}
	}
	rebuildScores, err := s.likes.ListMerchantLikeCounts(ctx, month, category, limit*hotOverfetch)
	if err != nil {
		if len(out.Items) > 0 {
			return out, nil
		}
		return HotMerchantList{}, fmt.Errorf("%w: merchant hot ranking unavailable", domain.ErrUnavailable)
	}
	rebuildItems := s.hotItems(ctx, rebuildScores, category, limit)
	if len(rebuildItems) > 0 {
		out.Items = rebuildItems
	}
	if s.hot != nil {
		for _, score := range rebuildScores {
			if err := s.hot.SetMerchantScore(ctx, month, category, score.MerchantID, score.LikeCount); err != nil {
				log.Printf("merchant hot rebuild: %v", err)
				break
			}
		}
	}
	return out, nil
}

// History returns a frozen monthly Top N from merchant_hot_snapshot. The
// current month is not frozen yet, so it delegates to the live Hot path; past
// months never consult Redis or current like counts.
func (s *MerchantLikeService) History(ctx context.Context, month, category string, limit int) (HotMerchantList, error) {
	month = strings.TrimSpace(month)
	if _, err := time.Parse("200601", month); err != nil {
		return HotMerchantList{}, fmt.Errorf("%w: month must be YYYYMM", domain.ErrInvalidRequest)
	}
	category = strings.ToUpper(strings.TrimSpace(category))
	if !merchant.ValidCategory(category) {
		return HotMerchantList{}, fmt.Errorf("%w: category must be FOOD, SUPPLIES or OTHER", domain.ErrInvalidRequest)
	}
	if limit <= 0 {
		limit = defaultHotLimit
	}
	if limit > maxHotLimit {
		limit = maxHotLimit
	}
	if month > MonthKey(s.now()) {
		return HotMerchantList{}, fmt.Errorf("%w: month cannot be in the future", domain.ErrInvalidRequest)
	}
	if month == MonthKey(s.now()) {
		return s.Hot(ctx, category, limit)
	}
	if s.snapshots == nil {
		return HotMerchantList{}, fmt.Errorf("%w: merchant hot snapshot store unavailable", domain.ErrUnavailable)
	}
	records, err := s.snapshots.ListMerchantHotSnapshot(ctx, month, category, limit)
	if err != nil {
		return HotMerchantList{}, fmt.Errorf("%w: merchant hot history unavailable", domain.ErrUnavailable)
	}
	out := HotMerchantList{Month: month, Category: category, Items: make([]HotMerchantItem, 0, len(records))}
	if len(records) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.MerchantID)
	}
	profiles := s.loadProfilesIncludingArchived(ctx, ids)
	for _, record := range records {
		profile := profiles[record.MerchantID]
		if profile == nil {
			// The snapshot deliberately outlives merchant rows. Keep the slot
			// visible with the frozen facts rather than silently shrinking history.
			out.Items = append(out.Items, HotMerchantItem{
				Rank: record.Rank, MerchantID: record.MerchantID, Name: "已下线商家",
				Category: category, LikeCount: record.LikeCount, BusinessStatus: "CLOSED",
			})
			continue
		}
		out.Items = append(out.Items, HotMerchantItem{
			Rank: record.Rank, MerchantID: profile.ID, Name: profile.Name,
			Category: category, LikeCount: record.LikeCount, Description: profile.Description,
			Address: profile.Address, AMAPPOIID: profile.AMAPPOIID,
			Latitude: profile.Latitude, Longitude: profile.Longitude,
			BusinessStatus: historicalBusinessStatus(profile), ServiceTypes: profile.ServiceTypes,
			CapacityPerHour: profile.CapacityPerHour,
		})
	}
	s.decorateHotItems(ctx, out.Items)
	return out, nil
}

// Rebuild is the scheduled, authoritative rebuild path. Unlike Hot, it never
// reads Redis first: it counts MySQL facts, filters ineligible merchants, writes
// the durable monthly snapshot transactionally, then refreshes Redis scores.
func (s *MerchantLikeService) Rebuild(ctx context.Context, categories []string, limit int) (HotRebuildResult, error) {
	if s.snapshots == nil {
		return HotRebuildResult{}, fmt.Errorf("%w: merchant hot snapshot store unavailable", domain.ErrUnavailable)
	}
	if limit <= 0 {
		limit = defaultHotLimit
	}
	if limit > maxHotLimit {
		limit = maxHotLimit
	}
	selected, err := normalizeHotCategories(categories)
	if err != nil {
		return HotRebuildResult{}, err
	}
	month := MonthKey(s.now())
	result := HotRebuildResult{Month: month, Categories: make(map[string]int, len(selected))}
	for _, category := range selected {
		counts, err := s.likes.ListMerchantLikeCounts(ctx, month, category, limit*hotOverfetch)
		if err != nil {
			return result, fmt.Errorf("%w: merchant hot rebuild counts unavailable", domain.ErrUnavailable)
		}
		items := s.hotItems(ctx, counts, category, limit)
		snapshotItems := make([]repository.MerchantHotSnapshotItem, 0, len(items))
		for _, item := range items {
			snapshotItems = append(snapshotItems, repository.MerchantHotSnapshotItem{
				Rank: item.Rank, MerchantID: item.MerchantID, LikeCount: item.LikeCount,
			})
		}
		if err := s.snapshots.ReplaceMerchantHotSnapshot(ctx, month, category, snapshotItems); err != nil {
			return result, fmt.Errorf("%w: persist merchant hot snapshot %s/%s", domain.ErrUnavailable, month, category)
		}
		if s.hot != nil {
			if err := s.hot.ClearMerchantScores(ctx, month, category); err != nil {
				return result, fmt.Errorf("%w: clear merchant hot scores %s/%s", domain.ErrUnavailable, month, category)
			}
			for _, item := range items {
				if err := s.hot.SetMerchantScore(ctx, month, category, item.MerchantID, item.LikeCount); err != nil {
					return result, fmt.Errorf("%w: write merchant hot score %s/%s/%s: %v", domain.ErrUnavailable, month, category, item.MerchantID, err)
				}
			}
		}
		result.Categories[category] = len(items)
		result.Items += len(items)
	}
	return result, nil
}

func normalizeHotCategories(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{"FOOD", "SUPPLIES", "OTHER"}, nil
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, value := range raw {
		category := strings.ToUpper(strings.TrimSpace(value))
		if !merchant.ValidCategory(category) {
			return nil, fmt.Errorf("%w: category must be FOOD, SUPPLIES or OTHER", domain.ErrInvalidRequest)
		}
		if seen[category] {
			continue
		}
		seen[category] = true
		out = append(out, category)
	}
	return out, nil
}

func (s *MerchantLikeService) project(ctx context.Context, month, category, merchantID string, count int64) {
	if s.hot == nil {
		return
	}
	if err := s.hot.SetMerchantScore(ctx, month, category, merchantID, count); err != nil {
		log.Printf("merchant hot score update %s/%s/%s: %v", month, category, merchantID, err)
	}
}

func (s *MerchantLikeService) hotItems(ctx context.Context, scores []repository.MerchantLikeCount, category string, limit int) []HotMerchantItem {
	ids := make([]string, 0, len(scores))
	for _, score := range scores {
		ids = append(ids, score.MerchantID)
	}
	profiles := s.loadProfiles(ctx, ids)
	items := make([]HotMerchantItem, 0, len(scores))
	for _, score := range scores {
		profile := profiles[score.MerchantID]
		if profile == nil || profile.Archived || profile.BusinessStatus == "CLOSED" || profile.Category() != category {
			continue
		}
		items = append(items, HotMerchantItem{
			Rank:            len(items) + 1,
			MerchantID:      profile.ID,
			Name:            profile.Name,
			Category:        profile.Category(),
			LikeCount:       score.LikeCount,
			Description:     profile.Description,
			Address:         profile.Address,
			AMAPPOIID:       profile.AMAPPOIID,
			Latitude:        profile.Latitude,
			Longitude:       profile.Longitude,
			BusinessStatus:  profile.BusinessStatus,
			ServiceTypes:    profile.ServiceTypes,
			CapacityPerHour: profile.CapacityPerHour,
		})
		if len(items) == limit {
			break
		}
	}
	s.decorateHotItems(ctx, items)
	return items
}

// decorateHotItems joins the card-only facts (avatar, rating summary and the
// highest-liked review) in two batched reads. A failure degrades the card copy,
// never the ranking itself.
func (s *MerchantLikeService) decorateHotItems(ctx context.Context, items []HotMerchantItem) {
	if s.cards == nil || len(items) == 0 {
		return
	}
	ids := make([]string, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].MerchantID)
	}
	if avatars, err := s.cards.MerchantAvatars(ctx, ids); err != nil {
		log.Printf("merchant hot avatars: %v", err)
	} else {
		for i := range items {
			items[i].AvatarURL = avatars[items[i].MerchantID]
		}
	}
	cards, err := s.cards.MerchantTopReviews(ctx, ids)
	if err != nil {
		log.Printf("merchant hot reviews: %v", err)
		return
	}
	for i := range items {
		card, ok := cards[items[i].MerchantID]
		if !ok {
			continue
		}
		items[i].ReviewCount = card.ReviewCount
		items[i].Rating = float64(int(card.Rating*10+0.5)) / 10
		label := strings.TrimSpace(card.Review.AuthorLabel)
		if label == "" {
			if card.Review.Anonymous {
				label = "匿名用户"
			} else {
				label = "CityFlow 用户"
			}
		}
		items[i].TopReview = &HotMerchantReview{
			ID:          card.Review.ID,
			AuthorLabel: label,
			Anonymous:   card.Review.Anonymous,
			Rating:      card.Review.Rating,
			Comment:     card.Review.Comment,
			Likes:       card.Review.Likes,
			Date:        hotReviewDate(card.Review.CreatedAt),
		}
	}
}

func hotReviewDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.In(cst).Format("2006-01-02")
}

// historicalBusinessStatus keeps the historical card closed when a profile was
// archived after the snapshot was written, even if its last stored status was
// still OPEN.
func historicalBusinessStatus(profile *merchant.Profile) string {
	if profile.Archived {
		return "CLOSED"
	}
	return profile.BusinessStatus
}

func (s *MerchantLikeService) loadProfilesIncludingArchived(ctx context.Context, ids []string) map[string]*merchant.Profile {
	profiles := make(map[string]*merchant.Profile, len(ids))
	if len(ids) == 0 || s.profiles == nil {
		return profiles
	}
	loaded, err := s.profiles.GetMerchantsByIDsIncludingArchived(ctx, ids)
	if err != nil {
		log.Printf("merchant hot history profiles: %v", err)
		return profiles
	}
	for _, profile := range loaded {
		profiles[profile.ID] = profile
	}
	return profiles
}

func (s *MerchantLikeService) loadProfiles(ctx context.Context, ids []string) map[string]*merchant.Profile {
	profiles := make(map[string]*merchant.Profile, len(ids))
	if len(ids) == 0 {
		return profiles
	}
	misses := ids
	if s.hot != nil {
		hits, missed, err := s.hot.GetMerchantProfiles(ctx, ids)
		if err != nil {
			log.Printf("merchant hot profile cache: %v", err)
		} else {
			for id, profile := range hits {
				profiles[id] = profile
			}
			misses = missed
		}
	}
	if len(misses) == 0 {
		return profiles
	}
	loaded, err := s.profiles.GetMerchantsByIDs(ctx, misses)
	if err != nil {
		log.Printf("merchant hot profile batch: %v", err)
		return profiles
	}
	for _, profile := range loaded {
		profiles[profile.ID] = profile
	}
	if s.hot != nil {
		if err := s.hot.CacheMerchantProfiles(ctx, loaded, 30*time.Minute); err != nil {
			log.Printf("merchant hot profile cache write: %v", err)
		}
	}
	return profiles
}

func uniqueMerchantIDs(raw []string, max int) []string {
	if max <= 0 {
		max = maxHotLimit
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == max {
			break
		}
	}
	return out
}

func (s *MerchantLikeService) merchantLock(merchantID string) *sync.Mutex {
	value, _ := s.locks.LoadOrStore(merchantID, &sync.Mutex{})
	return value.(*sync.Mutex)
}
