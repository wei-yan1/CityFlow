// Package redis implements Discovery's hot-merchant read model. The ZSET holds
// only merchant ids and absolute monthly like counts; merchant details live in
// a separate string cache with a short TTL.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"cityflow/app/discovery/domain/merchant"
	"cityflow/app/discovery/repository"
)

const (
	hotMerchantPrefix   = "hot:merchant:"
	merchantInfoPrefix  = "merchant:info:"
	defaultProfileTTL   = 30 * time.Minute
	defaultHotListLimit = 10
)

type HotStore struct {
	client *goredis.Client
}

func NewHotStore(client *goredis.Client) *HotStore {
	return &HotStore{client: client}
}

var _ repository.MerchantHotRanking = (*HotStore)(nil)

func (s *HotStore) SetMerchantScore(ctx context.Context, month, category, merchantID string, score int64) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("merchant hot redis is not configured")
	}
	if score <= 0 {
		return s.client.ZRem(ctx, hotKey(month, category), merchantID).Err()
	}
	return s.client.ZAdd(ctx, hotKey(month, category), goredis.Z{
		Score:  float64(score),
		Member: merchantID,
	}).Err()
}

// ClearMerchantScores removes the whole monthly partition before an
// authoritative rebuild. Without it, merchants that dropped out of MySQL could
// remain visible through the old ZSET.
func (s *HotStore) ClearMerchantScores(ctx context.Context, month, category string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("merchant hot redis is not configured")
	}
	return s.client.Del(ctx, hotKey(month, category)).Err()
}

func (s *HotStore) TopMerchantScores(ctx context.Context, month, category string, limit int) ([]repository.MerchantLikeCount, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("merchant hot redis is not configured")
	}
	if limit <= 0 {
		limit = defaultHotListLimit
	}
	values, err := s.client.ZRevRangeWithScores(ctx, hotKey(month, category), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]repository.MerchantLikeCount, 0, len(values))
	for _, value := range values {
		id, ok := value.Member.(string)
		if !ok {
			continue
		}
		out = append(out, repository.MerchantLikeCount{
			MerchantID: id,
			LikeCount:  int64(value.Score),
		})
	}
	return out, nil
}

func (s *HotStore) GetMerchantProfiles(ctx context.Context, ids []string) (map[string]*merchant.Profile, []string, error) {
	if s == nil || s.client == nil {
		return nil, nil, fmt.Errorf("merchant hot redis is not configured")
	}
	if len(ids) == 0 {
		return map[string]*merchant.Profile{}, nil, nil
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, merchantInfoPrefix+id)
	}
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, nil, err
	}
	hits := make(map[string]*merchant.Profile, len(ids))
	misses := make([]string, 0, len(ids))
	for i, value := range values {
		if value == nil {
			misses = append(misses, ids[i])
			continue
		}
		raw, ok := value.(string)
		if !ok || strings.TrimSpace(raw) == "" {
			misses = append(misses, ids[i])
			continue
		}
		var profile merchant.Profile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			misses = append(misses, ids[i])
			continue
		}
		hits[ids[i]] = &profile
	}
	return hits, misses, nil
}

func (s *HotStore) CacheMerchantProfiles(ctx context.Context, profiles []*merchant.Profile, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("merchant hot redis is not configured")
	}
	if len(profiles) == 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultProfileTTL
	}
	pipe := s.client.Pipeline()
	for _, profile := range profiles {
		if profile == nil || strings.TrimSpace(profile.ID) == "" {
			continue
		}
		raw, err := json.Marshal(profile)
		if err != nil {
			continue
		}
		pipe.Set(ctx, merchantInfoPrefix+profile.ID, raw, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *HotStore) InvalidateMerchantProfile(ctx context.Context, merchantID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("merchant hot redis is not configured")
	}
	if strings.TrimSpace(merchantID) == "" {
		return nil
	}
	return s.client.Del(ctx, merchantInfoPrefix+merchantID).Err()
}

func (s *HotStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("merchant hot redis is not configured")
	}
	return s.client.Ping(ctx).Err()
}

func hotKey(month, category string) string {
	return hotMerchantPrefix + month + ":" + category
}
