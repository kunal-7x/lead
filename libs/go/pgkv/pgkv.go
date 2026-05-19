package pgkv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists typed service records as JSONB in a service-local schema.
type Store struct {
	pool   *pgxpool.Pool
	schema string
	table  string
}

func New(ctx context.Context, dsn, schema string) (*Store, error) {
	schema = strings.TrimPrefix(schema, "svc_")
	if strings.TrimSpace(schema) == "" {
		return nil, fmt.Errorf("pgkv: schema is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgkv: connect: %w", err)
	}
	s := &Store{
		pool:   pool,
		schema: schema,
		table:  pgx.Identifier{schema, "c1_store_records"}.Sanitize(),
	}
	if err := s.Ensure(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgkv: ping: %w", err)
	}
	return s, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ensure(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`
CREATE SCHEMA IF NOT EXISTS %s;
CREATE TABLE IF NOT EXISTS %s (
	kind TEXT NOT NULL,
	key TEXT NOT NULL,
	tenant_id TEXT NOT NULL DEFAULT '',
	data JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (kind, key)
);
CREATE INDEX IF NOT EXISTS %s ON %s (kind, tenant_id, created_at);
`,
		pgx.Identifier{s.schema}.Sanitize(),
		s.table,
		pgx.Identifier{"c1_store_records_tenant_idx"}.Sanitize(),
		s.table,
	))
	if err != nil {
		return fmt.Errorf("pgkv: ensure schema: %w", err)
	}
	return nil
}

func (s *Store) Put(ctx context.Context, collection, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("pgkv: marshal %s/%s: %w", collection, key, err)
	}
	_, err = s.pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s (kind, key, data)
VALUES ($1, $2, $3::jsonb)
ON CONFLICT (kind, key) DO UPDATE
SET data = EXCLUDED.data, updated_at = now()
`, s.table), collection, key, data)
	if err != nil {
		return fmt.Errorf("pgkv: put %s/%s: %w", collection, key, err)
	}
	return nil
}

func (s *Store) Insert(ctx context.Context, collection, key string, value any) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("pgkv: marshal %s/%s: %w", collection, key, err)
	}
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s (kind, key, data)
VALUES ($1, $2, $3::jsonb)
ON CONFLICT (kind, key) DO NOTHING
`, s.table), collection, key, data)
	if err != nil {
		return false, fmt.Errorf("pgkv: insert %s/%s: %w", collection, key, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) Delete(ctx context.Context, collection, key string) error {
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE kind = $1 AND key = $2`, s.table), collection, key)
	if err != nil {
		return fmt.Errorf("pgkv: delete %s/%s: %w", collection, key, err)
	}
	return nil
}

func Get[T any](ctx context.Context, s *Store, collection, key string) (T, bool, error) {
	var zero T
	var raw []byte
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT data FROM %s WHERE kind = $1 AND key = $2`, s.table), collection, key).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("pgkv: get %s/%s: %w", collection, key, err)
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, false, fmt.Errorf("pgkv: unmarshal %s/%s: %w", collection, key, err)
	}
	return value, true, nil
}

func List[T any](ctx context.Context, s *Store, collection string) ([]T, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT data FROM %s WHERE kind = $1 ORDER BY created_at, key`, s.table), collection)
	if err != nil {
		return nil, fmt.Errorf("pgkv: list %s: %w", collection, err)
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("pgkv: scan %s: %w", collection, err)
		}
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("pgkv: unmarshal %s: %w", collection, err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgkv: rows %s: %w", collection, err)
	}
	return out, nil
}

func Map[T any](ctx context.Context, s *Store, collection string) (map[string]T, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT key, data FROM %s WHERE kind = $1 ORDER BY key`, s.table), collection)
	if err != nil {
		return nil, fmt.Errorf("pgkv: map %s: %w", collection, err)
	}
	defer rows.Close()

	out := map[string]T{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("pgkv: scan map %s: %w", collection, err)
		}
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("pgkv: unmarshal map %s/%s: %w", collection, key, err)
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgkv: rows map %s: %w", collection, err)
	}
	return out, nil
}

func NewID(prefix string) string {
	id := uuid.NewString()
	if prefix == "" {
		return id
	}
	return prefix + "-" + id
}
