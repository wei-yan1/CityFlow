package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const activeStatusKeyPrefix = "cityflow:auth:active:"

type Redis struct {
	client *redis.Client
}

func NewRedis(address, password string, database int) *Redis {
	return &Redis{client: redis.NewClient(&redis.Options{
		Addr: address, Password: password, DB: database,
		DialTimeout:  500 * time.Millisecond,
		ReadTimeout:  300 * time.Millisecond,
		WriteTimeout: 300 * time.Millisecond,
		PoolTimeout:  500 * time.Millisecond,
		MaxRetries:   1,
	})}
}

func (r *Redis) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("Redis client is not configured")
	}
	return r.client.Ping(ctx).Err()
}

func (r *Redis) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

// Client exposes the underlying client to service-owned Redis adapters.
func (r *Redis) Client() *redis.Client {
	if r == nil {
		return nil
	}
	return r.client
}

func (r *Redis) GetActive(ctx context.Context, userID string) (bool, bool, error) {
	value, err := r.client.Get(ctx, activeStatusKeyPrefix+userID).Result()
	if err == redis.Nil {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	active, err := strconv.ParseBool(value)
	if err != nil {
		return false, false, fmt.Errorf("decode cached account status: %w", err)
	}
	return active, true, nil
}

func (r *Redis) SetActive(ctx context.Context, userID string, active bool, ttl time.Duration) error {
	return r.client.Set(ctx, activeStatusKeyPrefix+userID, strconv.FormatBool(active), ttl).Err()
}

func (r *Redis) InvalidateActive(ctx context.Context, userID string) error {
	return r.client.Del(ctx, activeStatusKeyPrefix+userID).Err()
}
