package merchantservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/app/discovery/repository"
	"cityflow/internal/common/location"
)

type fakeProfiles struct {
	items map[string]*merchant.Profile
}

func (f *fakeProfiles) MapMerchants(context.Context, location.Bounds, int) ([]location.Place, error) {
	return nil, nil
}

func (f *fakeProfiles) MerchantForUser(context.Context, string) (*merchant.Profile, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProfiles) GetMerchant(_ context.Context, id string) (*merchant.Profile, error) {
	if item, ok := f.items[id]; ok {
		return item, nil
	}
	return nil, fmt.Errorf("merchant not found")
}

func (f *fakeProfiles) GetMerchantsByIDs(_ context.Context, ids []string) ([]*merchant.Profile, error) {
	out := make([]*merchant.Profile, 0, len(ids))
	for _, id := range ids {
		if item, ok := f.items[id]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

func (f *fakeProfiles) GetMerchantsByIDsIncludingArchived(ctx context.Context, ids []string) ([]*merchant.Profile, error) {
	return f.GetMerchantsByIDs(ctx, ids)
}

func (f *fakeProfiles) FindMerchantByAMapPOI(context.Context, string) (*merchant.Profile, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProfiles) FindMerchantNearName(context.Context, string, location.Point, float64) (*merchant.Profile, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProfiles) ListMerchants(context.Context, string, repository.Page) ([]*merchant.Profile, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProfiles) CreateMerchant(context.Context, string, merchant.UpdateRequest) (*merchant.Profile, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProfiles) UpdateMerchant(context.Context, string, merchant.UpdateRequest) (*merchant.Profile, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProfiles) ArchiveMerchant(context.Context, string) error {
	return errors.New("not implemented")
}

type fakeLikes struct {
	records map[string]bool
}

func newFakeLikes() *fakeLikes { return &fakeLikes{records: map[string]bool{}} }

func likeKey(merchantID, userID, month string) string {
	return merchantID + ":" + userID + ":" + month
}

func (f *fakeLikes) AddMerchantLike(_ context.Context, merchantID, userID, month string) (bool, error) {
	key := likeKey(merchantID, userID, month)
	if f.records[key] {
		return false, nil
	}
	f.records[key] = true
	return true, nil
}

func (f *fakeLikes) RemoveMerchantLike(_ context.Context, merchantID, userID, month string) (bool, error) {
	key := likeKey(merchantID, userID, month)
	if !f.records[key] {
		return false, nil
	}
	delete(f.records, key)
	return true, nil
}

func (f *fakeLikes) MerchantLikeCount(_ context.Context, merchantID, month string) (int64, error) {
	var count int64
	for key := range f.records {
		if keyMerchant(key) == merchantID && keyMonth(key) == month {
			count++
		}
	}
	return count, nil
}

func (f *fakeLikes) MerchantLikeCounts(_ context.Context, month string, ids []string) (map[string]int64, error) {
	out := make(map[string]int64, len(ids))
	for key := range f.records {
		id := keyMerchant(key)
		if keyMonth(key) == month && contains(ids, id) {
			out[id]++
		}
	}
	return out, nil
}

func (f *fakeLikes) MerchantLikeUsers(_ context.Context, userID, month string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	for key := range f.records {
		id := keyMerchant(key)
		if keyMonth(key) == month && keyUser(key) == userID && contains(ids, id) {
			out[id] = true
		}
	}
	return out, nil
}

func (f *fakeLikes) MerchantLikeStats(_ context.Context, userID, month string) (repository.MerchantLikeTotals, error) {
	var totals repository.MerchantLikeTotals
	for key := range f.records {
		if keyUser(key) != userID {
			continue
		}
		totals.Lifetime++
		if keyMonth(key) == month {
			totals.Monthly++
		}
	}
	return totals, nil
}

func (f *fakeLikes) ListMerchantLikeCounts(_ context.Context, month, category string, limit int) ([]repository.MerchantLikeCount, error) {
	counts := map[string]int64{}
	for key := range f.records {
		if keyMonth(key) == month {
			counts[keyMerchant(key)]++
		}
	}
	out := make([]repository.MerchantLikeCount, 0, len(counts))
	for id, count := range counts {
		out = append(out, repository.MerchantLikeCount{MerchantID: id, LikeCount: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LikeCount == out[j].LikeCount {
			return out[i].MerchantID < out[j].MerchantID
		}
		return out[i].LikeCount > out[j].LikeCount
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type fakeHot struct {
	scores  map[string]int64
	cache   map[string]*merchant.Profile
	updates int
}

func newFakeHot() *fakeHot {
	return &fakeHot{scores: map[string]int64{}, cache: map[string]*merchant.Profile{}}
}

func (f *fakeHot) SetMerchantScore(_ context.Context, _ string, _ string, id string, score int64) error {
	f.scores[id] = score
	f.updates++
	return nil
}

func (f *fakeHot) ClearMerchantScores(_ context.Context, _ string, _ string) error {
	f.scores = map[string]int64{}
	return nil
}

func (f *fakeHot) TopMerchantScores(_ context.Context, _ string, _ string, limit int) ([]repository.MerchantLikeCount, error) {
	out := make([]repository.MerchantLikeCount, 0, len(f.scores))
	for id, score := range f.scores {
		out = append(out, repository.MerchantLikeCount{MerchantID: id, LikeCount: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LikeCount == out[j].LikeCount {
			return out[i].MerchantID < out[j].MerchantID
		}
		return out[i].LikeCount > out[j].LikeCount
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeHot) GetMerchantProfiles(_ context.Context, ids []string) (map[string]*merchant.Profile, []string, error) {
	hits := map[string]*merchant.Profile{}
	misses := []string{}
	for _, id := range ids {
		if item, ok := f.cache[id]; ok {
			hits[id] = item
		} else {
			misses = append(misses, id)
		}
	}
	return hits, misses, nil
}

func (f *fakeHot) CacheMerchantProfiles(_ context.Context, profiles []*merchant.Profile, _ time.Duration) error {
	for _, item := range profiles {
		f.cache[item.ID] = item
	}
	return nil
}

func (f *fakeHot) InvalidateMerchantProfile(_ context.Context, id string) error {
	delete(f.cache, id)
	return nil
}

func (f *fakeHot) Ping(context.Context) error { return nil }

func keyMerchant(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i]
		}
	}
	return key
}

func keyUser(key string) string {
	first := -1
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			first = i
			break
		}
	}
	if first < 0 {
		return ""
	}
	for i := first + 1; i < len(key); i++ {
		if key[i] == ':' {
			return key[first+1 : i]
		}
	}
	return ""
}

func keyMonth(key string) string {
	last := -1
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == ':' {
			last = i
			break
		}
	}
	if last < 0 {
		return ""
	}
	return key[last+1:]
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type fakeCards struct {
	avatars map[string]string
	reviews map[string]repository.MerchantReviewCard
}

func newFakeCards() *fakeCards {
	return &fakeCards{avatars: map[string]string{}, reviews: map[string]repository.MerchantReviewCard{}}
}

func (f *fakeCards) MerchantAvatars(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if avatar, ok := f.avatars[id]; ok {
			out[id] = avatar
		}
	}
	return out, nil
}

func (f *fakeCards) MerchantTopReviews(_ context.Context, ids []string) (map[string]repository.MerchantReviewCard, error) {
	out := map[string]repository.MerchantReviewCard{}
	for _, id := range ids {
		if review, ok := f.reviews[id]; ok {
			out[id] = review
		}
	}
	return out, nil
}

func TestMerchantLikeSetIsMonthlyAndIdempotent(t *testing.T) {
	profiles := &fakeProfiles{items: map[string]*merchant.Profile{
		"m1": {ID: "m1", Name: "Food One", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN"},
	}}
	likes := newFakeLikes()
	hot := newFakeHot()
	svc := NewMerchantLikeService(likes, profiles, hot, newFakeCards())
	svc.now = func() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

	state, err := svc.Set(context.Background(), "m1", "u1", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 1 || state.Month != "202611" {
		t.Fatalf("unexpected first like: %+v", state)
	}
	state, err = svc.Set(context.Background(), "m1", "u2", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 2 {
		t.Fatalf("unexpected second like: %+v", state)
	}
	state, err = svc.Set(context.Background(), "m1", "u1", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 2 || hot.updates != 2 {
		t.Fatalf("duplicate like should not update score: state=%+v updates=%d", state, hot.updates)
	}
	state, err = svc.Set(context.Background(), "m1", "u1", false)
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 1 || hot.scores["m1"] != 1 {
		t.Fatalf("unexpected unlike: state=%+v score=%d", state, hot.scores["m1"])
	}
}

func TestMerchantHotFiltersIneligibleAndRebuildsFromDB(t *testing.T) {
	profiles := &fakeProfiles{items: map[string]*merchant.Profile{
		"m1": {ID: "m1", Name: "Food One", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN"},
		"m2": {ID: "m2", Name: "Closed Food", ServiceTypes: []string{"FOOD"}, BusinessStatus: "CLOSED"},
		"m3": {ID: "m3", Name: "Supplies", ServiceTypes: []string{"SUPPLIES"}, BusinessStatus: "OPEN"},
		"m4": {ID: "m4", Name: "Other", ServiceTypes: []string{"OTHER"}, BusinessStatus: "OPEN"},
		"m5": {ID: "m5", Name: "Food Two", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN"},
	}}
	likes := newFakeLikes()
	hot := newFakeHot()
	svc := NewMerchantLikeService(likes, profiles, hot, newFakeCards())
	svc.now = func() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }
	hot.scores = map[string]int64{"m2": 99, "m4": 50, "m1": 3, "m5": 1}

	result, err := svc.Hot(context.Background(), "FOOD", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].MerchantID != "m1" || result.Items[1].MerchantID != "m5" {
		t.Fatalf("unexpected hot list: %+v", result)
	}

	hot.scores = map[string]int64{}
	for _, id := range []string{"m1", "m5"} {
		for _, user := range []string{"u1", "u2", "u3"} {
			_, _ = likes.AddMerchantLike(context.Background(), id, user, "202611")
		}
	}
	result, err = svc.Hot(context.Background(), "FOOD", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].MerchantID != "m1" {
		t.Fatalf("unexpected rebuilt hot list: %+v", result)
	}
}

func TestMerchantHotDecoratesAvatarRatingAndTopReview(t *testing.T) {
	profiles := &fakeProfiles{items: map[string]*merchant.Profile{
		"m1": {ID: "m1", Name: "Food One", Description: "一碗热汤", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN"},
	}}
	likes := newFakeLikes()
	hot := newFakeHot()
	cards := newFakeCards()
	cards.avatars["m1"] = "https://example.test/m1.jpg"
	cards.reviews["m1"] = repository.MerchantReviewCard{
		Review: merchant.Review{
			ID: "r1", AuthorLabel: "用户·abc123", Rating: 5,
			Comment: "汤很热，粉很滑。", Likes: 9,
			CreatedAt: time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC),
		},
		ReviewCount: 12, Rating: 4.83,
	}
	svc := NewMerchantLikeService(likes, profiles, hot, cards)
	svc.now = func() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }
	hot.scores = map[string]int64{"m1": 5}

	result, err := svc.Hot(context.Background(), "FOOD", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("unexpected hot list: %+v", result)
	}
	item := result.Items[0]
	if item.AvatarURL != "https://example.test/m1.jpg" || item.ReviewCount != 12 || item.Rating != 4.8 {
		t.Fatalf("unexpected card extras: %+v", item)
	}
	if item.TopReview == nil || item.TopReview.ID != "r1" || item.TopReview.Likes != 9 || item.TopReview.Date != "2026-11-19" {
		t.Fatalf("unexpected top review: %+v", item.TopReview)
	}
}

func TestMyMerchantLikeStatsSeparatesMonthAndLifetime(t *testing.T) {
	likes := newFakeLikes()
	likes.records[likeKey("m1", "u1", "202610")] = true
	likes.records[likeKey("m2", "u1", "202611")] = true
	likes.records[likeKey("m3", "u2", "202611")] = true
	svc := NewMerchantLikeService(likes, &fakeProfiles{}, newFakeHot(), newFakeCards())
	svc.now = func() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

	stats, err := svc.MyStats(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Month != "202611" || stats.Monthly != 1 || stats.Total != 2 {
		t.Fatalf("unexpected my stats: %+v", stats)
	}
}

func TestMerchantServiceRejectsLegacyServiceTypes(t *testing.T) {
	_, err := normalizeMerchantRequest(merchant.UpdateRequest{
		Name: "shop", Address: "road", Latitude: 1, Longitude: 2,
		BusinessStatus: "OPEN", ServiceTypes: []string{"DELIVERY"},
	})
	if err == nil {
		t.Fatal("legacy service type should be rejected")
	}
}

type fakeSnapshots struct {
	month      string
	category   string
	items      []repository.MerchantHotSnapshotItem
	calls      int
	history    []repository.MerchantHotSnapshotItem
	historyErr error
}

func (f *fakeSnapshots) ReplaceMerchantHotSnapshot(_ context.Context, month, category string, items []repository.MerchantHotSnapshotItem) error {
	f.month = month
	f.category = category
	f.items = append([]repository.MerchantHotSnapshotItem(nil), items...)
	f.calls++
	return nil
}

func (f *fakeSnapshots) ListMerchantHotSnapshot(_ context.Context, _, _ string, _ int) ([]repository.MerchantHotSnapshotItem, error) {
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	return append([]repository.MerchantHotSnapshotItem(nil), f.history...), nil
}

func TestMerchantHotRebuildPersistsSnapshotAndScores(t *testing.T) {
	profiles := &fakeProfiles{items: map[string]*merchant.Profile{
		"m1": {ID: "m1", Name: "Food One", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN"},
		"m2": {ID: "m2", Name: "Closed Food", ServiceTypes: []string{"FOOD"}, BusinessStatus: "CLOSED"},
		"m3": {ID: "m3", Name: "Food Three", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN"},
	}}
	likes := newFakeLikes()
	for _, record := range [][2]string{
		{"m1", "u1"}, {"m1", "u2"}, {"m2", "u3"}, {"m3", "u4"},
	} {
		_, _ = likes.AddMerchantLike(context.Background(), record[0], record[1], "202611")
	}
	hot := newFakeHot()
	snapshots := &fakeSnapshots{}
	svc := NewMerchantLikeService(likes, profiles, hot, newFakeCards())
	svc.SetHotSnapshot(snapshots)
	svc.now = func() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

	result, err := svc.Rebuild(context.Background(), []string{"FOOD"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Month != "202611" || result.Categories["FOOD"] != 2 || result.Items != 2 {
		t.Fatalf("unexpected rebuild result: %+v", result)
	}
	if snapshots.calls != 1 || snapshots.category != "FOOD" || len(snapshots.items) != 2 {
		t.Fatalf("unexpected snapshot write: %+v", snapshots)
	}
	if snapshots.items[0].MerchantID != "m1" || snapshots.items[0].Rank != 1 || snapshots.items[0].LikeCount != 2 {
		t.Fatalf("unexpected first snapshot item: %+v", snapshots.items[0])
	}
	if snapshots.items[1].MerchantID != "m3" || snapshots.items[1].Rank != 2 || snapshots.items[1].LikeCount != 1 {
		t.Fatalf("unexpected second snapshot item: %+v", snapshots.items[1])
	}
	if hot.scores["m1"] != 2 || hot.scores["m3"] != 1 {
		t.Fatalf("unexpected rebuilt scores: %+v", hot.scores)
	}
	if _, ok := hot.scores["m2"]; ok {
		t.Fatalf("closed merchant should not be projected: %+v", hot.scores)
	}
}

func TestMerchantHotRebuildRejectsUnknownCategory(t *testing.T) {
	svc := NewMerchantLikeService(newFakeLikes(), &fakeProfiles{}, newFakeHot(), newFakeCards())
	svc.SetHotSnapshot(&fakeSnapshots{})
	if _, err := svc.Rebuild(context.Background(), []string{"DELIVERY"}, 10); err == nil {
		t.Fatal("unknown category should be rejected")
	}
}

func TestMerchantHotHistoryReadsSnapshotIncludingArchived(t *testing.T) {
	profiles := &fakeProfiles{items: map[string]*merchant.Profile{
		"m1": {ID: "m1", Name: "Archived Food", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN", Archived: true},
		"m2": {ID: "m2", Name: "Food Two", ServiceTypes: []string{"FOOD"}, BusinessStatus: "OPEN"},
	}}
	snapshots := &fakeSnapshots{history: []repository.MerchantHotSnapshotItem{
		{Rank: 1, MerchantID: "m1", LikeCount: 9},
		{Rank: 2, MerchantID: "m2", LikeCount: 4},
		{Rank: 3, MerchantID: "gone", LikeCount: 2},
	}}
	svc := NewMerchantLikeService(newFakeLikes(), profiles, newFakeHot(), newFakeCards())
	svc.SetHotSnapshot(snapshots)
	svc.now = func() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

	result, err := svc.History(context.Background(), "202610", "FOOD", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Month != "202610" || result.Category != "FOOD" || len(result.Items) != 3 {
		t.Fatalf("unexpected history result: %+v", result)
	}
	if result.Items[0].Name != "Archived Food" || result.Items[0].Rank != 1 || result.Items[0].LikeCount != 9 {
		t.Fatalf("archived snapshot item was not preserved: %+v", result.Items[0])
	}
	if result.Items[0].BusinessStatus != "CLOSED" {
		t.Fatalf("archived merchant should render closed: %+v", result.Items[0])
	}
	if result.Items[2].Name != "已下线商家" || result.Items[2].Rank != 3 {
		t.Fatalf("missing merchant placeholder is wrong: %+v", result.Items[2])
	}
}

func TestMerchantHotHistoryRejectsInvalidAndFutureMonth(t *testing.T) {
	svc := NewMerchantLikeService(newFakeLikes(), &fakeProfiles{}, newFakeHot(), newFakeCards())
	svc.SetHotSnapshot(&fakeSnapshots{})
	svc.now = func() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }
	if _, err := svc.History(context.Background(), "2026-10", "FOOD", 10); err == nil {
		t.Fatal("non-YYYYMM month should be rejected")
	}
	if _, err := svc.History(context.Background(), "202612", "FOOD", 10); err == nil {
		t.Fatal("future month should be rejected")
	}
}
