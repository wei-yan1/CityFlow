package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"cityflow/internal/domain"
	"github.com/redis/go-redis/v9"
)

// Refresh sessions store only token hashes, never bearer credentials. All keys
// use one Redis hash slot so rotation is atomic in Redis Cluster as well.
var ErrRefreshInvalid = domain.ErrUnauthorized

func refreshKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "cityflow:{refresh-sessions}:" + hex.EncodeToString(digest[:])
}

func (r *Redis) CreateRefreshSession(ctx context.Context, token, userID string, ttl time.Duration) error {
	if token == "" || userID == "" || ttl <= 0 {
		return ErrRefreshInvalid
	}
	created, err := r.client.SetNX(ctx, refreshKey(token), userID, ttl).Result()
	if err != nil {
		return err
	}
	if !created {
		return ErrRefreshInvalid
	}
	return nil
}

func (r *Redis) GetRefreshSession(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrRefreshInvalid
	}
	id, err := r.client.Get(ctx, refreshKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrRefreshInvalid
	}
	return id, err
}

// RotateRefreshSession consumes the old token and creates its replacement in
// a single Redis operation. Exactly one concurrent caller can succeed.
func (r *Redis) RotateRefreshSession(ctx context.Context, oldToken, newToken string, ttl time.Duration) (string, error) {
	if oldToken == "" || newToken == "" || oldToken == newToken || ttl <= 0 {
		return "", ErrRefreshInvalid
	}
	const script = `local user=redis.call('GET',KEYS[1]);if not user or redis.call('EXISTS',KEYS[2])==1 then return false end;redis.call('SET',KEYS[2],user,'PX',ARGV[1]);redis.call('DEL',KEYS[1]);return user`
	user, err := r.client.Eval(ctx, script, []string{refreshKey(oldToken), refreshKey(newToken)}, ttl.Milliseconds()).Text()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrRefreshInvalid
		}
		return "", err
	}
	return user, nil
}

func (r *Redis) RevokeRefreshSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return r.client.Del(ctx, refreshKey(token)).Err()
}
