package store

import (
	"context"
	"strings"

	"github.com/lead/libs/go/pgkv"
)

type PostgresStore struct {
	kv *pgkv.Store
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	kv, err := pgkv.New(context.Background(), dsn, "svc_model_config")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{kv: kv}, nil
}

func (p *PostgresStore) Close() { p.kv.Close() }

func (p *PostgresStore) Get(ctx context.Context, key string) (string, bool, error) {
	value, ok, err := pgkv.Get[string](ctx, p.kv, "config", key)
	return value, ok, err
}

func (p *PostgresStore) Set(ctx context.Context, key string, value string) error {
	return p.kv.Put(ctx, "config", key, value)
}

func (p *PostgresStore) Delete(ctx context.Context, key string) error {
	return p.kv.Delete(ctx, "config", key)
}

func (p *PostgresStore) Keys(ctx context.Context, prefix string) (map[string]string, error) {
	items, err := pgkv.Map[string](ctx, p.kv, "config")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for key, value := range items {
		if strings.HasPrefix(key, prefix) {
			out[key] = value
		}
	}
	return out, nil
}
