package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is the rate-limiting interface.
type Limiter interface {
	// Allow returns true if the request is within the allowed rate.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// Redis-backed rate limiter using a sliding window counter.
type Redis struct {
	rdb *redis.Client
}

func NewRedis(rdb *redis.Client) *Redis {
	return &Redis{rdb: rdb}
}

func (r *Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	rkey := fmt.Sprintf("rl:%s", key)
	pipe := r.rdb.Pipeline()
	incr := pipe.Incr(ctx, rkey)
	pipe.Expire(ctx, rkey, window)
	if _, err := pipe.Exec(ctx); err != nil {
		// fail open: let the request through if Redis is down
		return true, nil
	}
	return incr.Val() <= int64(limit), nil
}

// NoOp always allows — used in unit tests.
type NoOp struct{}

func (n *NoOp) Allow(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	return true, nil
}
