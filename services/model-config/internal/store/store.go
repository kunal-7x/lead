package store

import "context"

type Store interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
	Delete(ctx context.Context, key string) error
	Keys(ctx context.Context, prefix string) (map[string]string, error)
}
