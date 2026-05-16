package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool with tenant-scoped RLS helpers.
type Pool struct {
	*pgxpool.Pool
}

// New creates a pgx connection pool from the given DSN.
func New(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	return &Pool{Pool: pool}, nil
}

// SetTenantRLS sets the app.tenant_id session variable for Row-Level Security.
func (p *Pool) SetTenantRLS(ctx context.Context, conn *pgxpool.Conn, tenantID string) error {
	_, err := conn.Exec(ctx, "SET LOCAL app.tenant_id = $1", tenantID)
	return err
}

// WithTx executes fn inside a transaction; rolls back on error.
func (p *Pool) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// OutboxEntry is a transactional outbox row to be relayed to NATS.
type OutboxEntry struct {
	EventType string
	TenantID  string
	Payload   []byte
}

// WriteOutbox inserts an outbox entry within the given transaction.
func WriteOutbox(ctx context.Context, tx pgx.Tx, entry OutboxEntry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO outbox (event_type, tenant_id, payload, created_at)
		 VALUES ($1, $2, $3, NOW())`,
		entry.EventType, entry.TenantID, entry.Payload,
	)
	return err
}
