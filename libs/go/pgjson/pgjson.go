package pgjson

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var identPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func OpenPool(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	if !identPattern.MatchString(schema) {
		return nil, fmt.Errorf("invalid schema %q", schema)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	searchPath := fmt.Sprintf("%s,public", quoteIdent(schema))
	cfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+searchPath)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func Put(ctx context.Context, db *pgxpool.Pool, kind, key, tenantID string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", kind, key, err)
	}
	_, err = db.Exec(ctx, `
		INSERT INTO c1_store_records (kind, key, tenant_id, data)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (kind, key) DO UPDATE
		SET tenant_id = EXCLUDED.tenant_id,
		    data = EXCLUDED.data,
		    updated_at = now()
	`, kind, key, tenantID, string(data))
	if err != nil {
		return fmt.Errorf("put %s/%s: %w", kind, key, err)
	}
	return nil
}

func Insert(ctx context.Context, db *pgxpool.Pool, kind, key, tenantID string, value any) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("marshal %s/%s: %w", kind, key, err)
	}
	tag, err := db.Exec(ctx, `
		INSERT INTO c1_store_records (kind, key, tenant_id, data)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (kind, key) DO NOTHING
	`, kind, key, tenantID, string(data))
	if err != nil {
		return false, fmt.Errorf("insert %s/%s: %w", kind, key, err)
	}
	return tag.RowsAffected() == 1, nil
}

func Get[T any](ctx context.Context, db *pgxpool.Pool, kind, key string) (T, bool, error) {
	var zero T
	var data []byte
	err := db.QueryRow(ctx, `
		SELECT data
		FROM c1_store_records
		WHERE kind = $1 AND key = $2
	`, kind, key).Scan(&data)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("get %s/%s: %w", kind, key, err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, false, fmt.Errorf("unmarshal %s/%s: %w", kind, key, err)
	}
	return out, true, nil
}

func List[T any](ctx context.Context, db *pgxpool.Pool, kind, tenantID string) ([]T, error) {
	rows, err := db.Query(ctx, `
		SELECT data
		FROM c1_store_records
		WHERE kind = $1 AND ($2 = '' OR tenant_id = $2)
		ORDER BY created_at, key
	`, kind, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", kind, err)
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan %s: %w", kind, err)
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", kind, err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", kind, err)
	}
	return out, nil
}

func Delete(ctx context.Context, db *pgxpool.Pool, kind, key string) error {
	_, err := db.Exec(ctx, `DELETE FROM c1_store_records WHERE kind = $1 AND key = $2`, kind, key)
	if err != nil {
		return fmt.Errorf("delete %s/%s: %w", kind, key, err)
	}
	return nil
}

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	id := hex.EncodeToString(b[:])
	if prefix == "" {
		return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	}
	return prefix + "-" + id[:12]
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
