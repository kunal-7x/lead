package tenantcache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const ttl = 5 * time.Minute

// Cache resolves tenant slug → tenant_id using Redis as a look-aside cache.
type Cache struct {
	rdb *redis.Client
}

// New creates a Cache backed by Redis at addr.
func New(addr string) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		// Return cache in degraded mode — Resolve will always miss.
		return &Cache{rdb: rdb}, nil
	}
	return &Cache{rdb: rdb}, nil
}

// Resolve returns the tenant_id for slug, using Redis as L1 cache.
// On cache miss it returns an error; population happens via SetSlug.
func (c *Cache) Resolve(ctx context.Context, slug string) (string, error) {
	key := fmt.Sprintf("tenant:slug:%s", slug)
	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("slug %q not found in cache", slug)
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetSlug caches slug → tenantID with the default TTL.
func (c *Cache) SetSlug(ctx context.Context, slug, tenantID string) error {
	key := fmt.Sprintf("tenant:slug:%s", slug)
	return c.rdb.Set(ctx, key, tenantID, ttl).Err()
}
