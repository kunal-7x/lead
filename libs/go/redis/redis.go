package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Client wraps go-redis with per-tenant key namespacing.
type Client struct {
	rdb      *goredis.Client
	tenantID string
}

// New creates a Redis client connected to addr.
func New(addr string) *goredis.Client {
	return goredis.NewClient(&goredis.Options{Addr: addr})
}

// ForTenant returns a namespaced Client for the given tenant.
func ForTenant(rdb *goredis.Client, tenantID string) *Client {
	return &Client{rdb: rdb, tenantID: tenantID}
}

func (c *Client) key(k string) string {
	return fmt.Sprintf("t:%s:%s", c.tenantID, k)
}

// Set stores a value with optional TTL.
func (c *Client) Set(ctx context.Context, k string, v interface{}, ttl time.Duration) error {
	return c.rdb.Set(ctx, c.key(k), v, ttl).Err()
}

// Get retrieves a value by key.
func (c *Client) Get(ctx context.Context, k string) (string, error) {
	return c.rdb.Get(ctx, c.key(k)).Result()
}

// Del deletes keys.
func (c *Client) Del(ctx context.Context, keys ...string) error {
	namespaced := make([]string, len(keys))
	for i, k := range keys {
		namespaced[i] = c.key(k)
	}
	return c.rdb.Del(ctx, namespaced...).Err()
}
