// Package identityredis owns Identity-specific Redis keys. Generic
// connection/pooling stays in internal/platform/cache.
package identityredis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"cityflow/internal/common/domain"
	platformcache "cityflow/internal/platform/cache"
	"github.com/redis/go-redis/v9"
)

// ErrRefreshInvalid is returned for missing, replayed or malformed refresh
// tokens. Identity maps it to the common unauthorized response.
var ErrRefreshInvalid = domain.ErrUnauthorized

type RefreshStore struct {
	redis *platformcache.Redis
}

func NewRefreshStore(redis *platformcache.Redis) *RefreshStore {
	return &RefreshStore{redis: redis}
}

func refreshKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "cityflow:{refresh-sessions}:" + hex.EncodeToString(digest[:])
}

func (s *RefreshStore) CreateRefreshSession(ctx context.Context, token, userID string, ttl time.Duration) error {
	if s == nil || s.redis == nil || token == "" || userID == "" || ttl <= 0 {
		return ErrRefreshInvalid
	}
	created, err := s.redis.Client().SetNX(ctx, refreshKey(token), userID, ttl).Result()
	if err != nil {
		return err
	}
	if !created {
		return ErrRefreshInvalid
	}
	return nil
}

func (s *RefreshStore) GetRefreshSession(ctx context.Context, token string) (string, error) {
	if s == nil || s.redis == nil || token == "" {
		return "", ErrRefreshInvalid
	}
	id, err := s.redis.Client().Get(ctx, refreshKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrRefreshInvalid
	}
	return id, err
}

// RotateRefreshSession consumes the old token and creates its replacement in
// a single Redis operation. Exactly one concurrent caller can succeed.
func (s *RefreshStore) RotateRefreshSession(ctx context.Context, oldToken, newToken string, ttl time.Duration) (string, error) {
	if s == nil || s.redis == nil || oldToken == "" || newToken == "" || oldToken == newToken || ttl <= 0 {
		return "", ErrRefreshInvalid
	}
	const script = `local user=redis.call('GET',KEYS[1]);if not user or redis.call('EXISTS',KEYS[2])==1 then return false end;redis.call('SET',KEYS[2],user,'PX',ARGV[1]);redis.call('DEL',KEYS[1]);return user`
	user, err := s.redis.Client().Eval(ctx, script, []string{refreshKey(oldToken), refreshKey(newToken)}, ttl.Milliseconds()).Text()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrRefreshInvalid
		}
		return "", err
	}
	return user, nil
}

func (s *RefreshStore) RevokeRefreshSession(ctx context.Context, token string) error {
	if s == nil || s.redis == nil || token == "" {
		return nil
	}
	return s.redis.Client().Del(ctx, refreshKey(token)).Err()
}
