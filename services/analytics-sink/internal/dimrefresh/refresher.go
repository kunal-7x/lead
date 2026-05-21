package dimrefresh

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	chclient "github.com/lead/services/analytics-sink/internal/clickhouse"
)

type Refresher struct {
	pg *pgxpool.Pool
	ch *chclient.Client
}

func New(ctx context.Context, postgresDSN string, client *chclient.Client) (*Refresher, error) {
	pool, err := pgxpool.New(ctx, postgresDSN)
	if err != nil {
		return nil, fmt.Errorf("dim refresh: postgres connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("dim refresh: postgres ping: %w", err)
	}
	return &Refresher{pg: pool, ch: client}, nil
}

func (r *Refresher) Close() {
	if r.pg != nil {
		r.pg.Close()
	}
}

func (r *Refresher) Run(ctx context.Context, interval time.Duration, log *slog.Logger) {
	if interval == 0 {
		interval = 5 * time.Minute
	}
	if err := r.Refresh(ctx); err != nil && log != nil {
		log.Warn("analytics-sink: dim refresh failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Refresh(ctx); err != nil && log != nil {
				log.Warn("analytics-sink: dim refresh failed", "error", err)
			}
		}
	}
}

func (r *Refresher) Refresh(ctx context.Context) error {
	if err := r.refreshTenants(ctx); err != nil {
		return err
	}
	if err := r.refreshUsers(ctx); err != nil {
		return err
	}
	if err := r.refreshCampaigns(ctx); err != nil {
		return err
	}
	if err := r.refreshProjects(ctx); err != nil {
		return err
	}
	if err := r.refreshKBVersions(ctx); err != nil {
		return err
	}
	return nil
}

func (r *Refresher) refreshTenants(ctx context.Context) error {
	if ok, err := r.tableExists(ctx, "tenant_auth.tenants"); err != nil || !ok {
		return err
	}
	rows, err := r.pg.Query(ctx, `
SELECT id::text, name, updated_at
FROM tenant_auth.tenants
WHERE status != 'deleted'
`)
	if err != nil {
		return fmt.Errorf("dim tenants query: %w", err)
	}
	defer rows.Close()
	var batch [][]any
	for rows.Next() {
		var tenantID, name string
		var updatedAt time.Time
		if err := rows.Scan(&tenantID, &name, &updatedAt); err != nil {
			return err
		}
		batch = append(batch, []any{tenantID, name, updatedAt.UTC()})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.insertDim(ctx, "dim_tenants", []ch.ColumnNameAndType{
		{Name: "tenant_id", Type: "String"},
		{Name: "name", Type: "String"},
		{Name: "updated_at", Type: "DateTime64(3)"},
	}, batch)
}

func (r *Refresher) refreshUsers(ctx context.Context) error {
	if ok, err := r.tableExists(ctx, "tenant_auth.tenant_users"); err != nil || !ok {
		return err
	}
	rows, err := r.pg.Query(ctx, `
SELECT tenant_id::text, id::text, display_name, status, updated_at
FROM tenant_auth.tenant_users
WHERE status != 'deleted'
`)
	if err != nil {
		return fmt.Errorf("dim users query: %w", err)
	}
	defer rows.Close()
	var batch [][]any
	for rows.Next() {
		var tenantID, userID, name, role string
		var updatedAt time.Time
		if err := rows.Scan(&tenantID, &userID, &name, &role, &updatedAt); err != nil {
			return err
		}
		batch = append(batch, []any{tenantID, userID, name, role, updatedAt.UTC()})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.insertDim(ctx, "dim_users", []ch.ColumnNameAndType{
		{Name: "tenant_id", Type: "String"},
		{Name: "user_id", Type: "String"},
		{Name: "name", Type: "String"},
		{Name: "role", Type: "String"},
		{Name: "updated_at", Type: "DateTime64(3)"},
	}, batch)
}

func (r *Refresher) refreshCampaigns(ctx context.Context) error {
	if ok, err := r.tableExists(ctx, "campaign.c1_store_records"); err != nil || !ok {
		return err
	}
	rows, err := r.pg.Query(ctx, `
SELECT
	COALESCE(data->>'tenant_id', ''),
	key,
	COALESCE(NULLIF(data->>'name', ''), key),
	COALESCE(NULLIF(data->>'updated_at', '')::timestamptz, updated_at)
FROM campaign.c1_store_records
WHERE kind = 'campaigns'
`)
	if err != nil {
		return fmt.Errorf("dim campaigns query: %w", err)
	}
	defer rows.Close()
	var batch [][]any
	for rows.Next() {
		var tenantID, campaignID, name string
		var updatedAt time.Time
		if err := rows.Scan(&tenantID, &campaignID, &name, &updatedAt); err != nil {
			return err
		}
		batch = append(batch, []any{tenantID, campaignID, name, updatedAt.UTC()})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.insertDim(ctx, "dim_campaigns", []ch.ColumnNameAndType{
		{Name: "tenant_id", Type: "String"},
		{Name: "campaign_id", Type: "String"},
		{Name: "name", Type: "String"},
		{Name: "updated_at", Type: "DateTime64(3)"},
	}, batch)
}

func (r *Refresher) refreshProjects(ctx context.Context) error {
	if ok, err := r.tableExists(ctx, "knowledge.c1_store_records"); err != nil || !ok {
		return err
	}
	rows, err := r.pg.Query(ctx, `
SELECT
	COALESCE(data->>'tenant_id', ''),
	key,
	COALESCE(NULLIF(data->>'name', ''), key),
	COALESCE(NULLIF(data->>'updated_at', '')::timestamptz, updated_at)
FROM knowledge.c1_store_records
WHERE kind = 'projects'
`)
	if err != nil {
		return fmt.Errorf("dim projects query: %w", err)
	}
	defer rows.Close()
	var batch [][]any
	for rows.Next() {
		var tenantID, projectID, name string
		var updatedAt time.Time
		if err := rows.Scan(&tenantID, &projectID, &name, &updatedAt); err != nil {
			return err
		}
		batch = append(batch, []any{tenantID, projectID, name, updatedAt.UTC()})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.insertDim(ctx, "dim_projects", []ch.ColumnNameAndType{
		{Name: "tenant_id", Type: "String"},
		{Name: "project_id", Type: "String"},
		{Name: "name", Type: "String"},
		{Name: "updated_at", Type: "DateTime64(3)"},
	}, batch)
}

func (r *Refresher) refreshKBVersions(ctx context.Context) error {
	if ok, err := r.tableExists(ctx, "knowledge.c1_store_records"); err != nil || !ok {
		return err
	}
	rows, err := r.pg.Query(ctx, `
WITH projects AS (
	SELECT key AS project_id, COALESCE(data->>'tenant_id', '') AS tenant_id
	FROM knowledge.c1_store_records
	WHERE kind = 'projects'
)
SELECT
	COALESCE(projects.tenant_id, ''),
	versions.key,
	COALESCE(versions.data->>'project_id', ''),
	COALESCE(versions.data->>'status', ''),
	COALESCE(NULLIF(versions.data->>'updated_at', '')::timestamptz, versions.updated_at)
FROM knowledge.c1_store_records versions
LEFT JOIN projects ON projects.project_id = versions.data->>'project_id'
WHERE versions.kind = 'kb_versions'
`)
	if err != nil {
		return fmt.Errorf("dim kb_versions query: %w", err)
	}
	defer rows.Close()
	var batch [][]any
	for rows.Next() {
		var tenantID, versionID, projectID, status string
		var updatedAt time.Time
		if err := rows.Scan(&tenantID, &versionID, &projectID, &status, &updatedAt); err != nil {
			return err
		}
		batch = append(batch, []any{tenantID, versionID, projectID, status, updatedAt.UTC()})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return r.insertDim(ctx, "dim_kb_versions", []ch.ColumnNameAndType{
		{Name: "tenant_id", Type: "String"},
		{Name: "kb_version_id", Type: "String"},
		{Name: "project_id", Type: "String"},
		{Name: "status", Type: "String"},
		{Name: "updated_at", Type: "DateTime64(3)"},
	}, batch)
}

func (r *Refresher) tableExists(ctx context.Context, qualified string) (bool, error) {
	var exists bool
	if err := r.pg.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, qualified).Scan(&exists); err != nil {
		return false, fmt.Errorf("dim refresh: check table %s: %w", qualified, err)
	}
	return exists, nil
}

func (r *Refresher) insertDim(ctx context.Context, table string, columns []ch.ColumnNameAndType, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	ctx = ch.Context(ctx, ch.WithColumnNamesAndTypes(columns))
	batch, err := r.ch.Conn().PrepareBatch(ctx, fmt.Sprintf("INSERT INTO %s", r.ch.Qualified(table)))
	if err != nil {
		return fmt.Errorf("dim %s prepare: %w", table, err)
	}
	defer batch.Close()
	for _, row := range rows {
		if err := batch.Append(row...); err != nil {
			return fmt.Errorf("dim %s append: %w", table, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("dim %s send: %w", table, err)
	}
	return nil
}
