package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"
)

// OutboxRow is one pending event from a service's outbox_events table.
type OutboxRow struct {
	ID         string
	Subject    string
	Payload    []byte
	CreatedAt  time.Time
}

// OutboxStore reads/marks rows in a service's outbox_events table.
// Each service implements this against its own DB connection so that business
// writes and outbox inserts share the same transaction.
type OutboxStore interface {
	FetchUnpublished(ctx context.Context, limit int) ([]OutboxRow, error)
	MarkPublished(ctx context.Context, id string) error
}

// OutboxRelay drains an outbox table into a Publisher.
type OutboxRelay struct {
	Store     OutboxStore
	Publisher Publisher
	Interval  time.Duration
	BatchSize int
	Logger    *log.Logger
}

// Run blocks until ctx is cancelled, draining the outbox on Interval ticks.
func (r *OutboxRelay) Run(ctx context.Context) error {
	if r.Interval == 0 {
		r.Interval = 500 * time.Millisecond
	}
	if r.BatchSize == 0 {
		r.BatchSize = 100
	}
	if r.Logger == nil {
		r.Logger = log.Default()
	}

	t := time.NewTicker(r.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			r.drainOnce(ctx)
		}
	}
}

func (r *OutboxRelay) drainOnce(ctx context.Context) {
	rows, err := r.Store.FetchUnpublished(ctx, r.BatchSize)
	if err != nil {
		r.Logger.Printf("outbox: fetch: %v", err)
		return
	}
	for _, row := range rows {
		if err := r.Publisher.Publish(ctx, row.Subject, row.Payload, WithIdempotencyKey(row.ID)); err != nil {
			r.Logger.Printf("outbox: publish %s id=%s: %v", row.Subject, row.ID, err)
			continue
		}
		if err := r.Store.MarkPublished(ctx, row.ID); err != nil {
			r.Logger.Printf("outbox: mark %s: %v", row.ID, err)
		}
	}
}

// PGOutboxStore is a *sql.DB-backed OutboxStore that operates on the standard
// outbox_events table layout used across services.
type PGOutboxStore struct {
	DB    *sql.DB
	Table string // default "outbox_events"
}

const defaultOutboxTable = "outbox_events"

func (s *PGOutboxStore) table() string {
	if s.Table == "" {
		return defaultOutboxTable
	}
	return s.Table
}

// FetchUnpublished returns rows where published_at IS NULL, oldest first.
func (s *PGOutboxStore) FetchUnpublished(ctx context.Context, limit int) ([]OutboxRow, error) {
	if s.DB == nil {
		return nil, errors.New("outbox: nil db")
	}
	q := fmt.Sprintf(`SELECT id, subject, payload, created_at
		FROM %s
		WHERE published_at IS NULL
		ORDER BY created_at ASC
		LIMIT $1`, s.table())
	rows, err := s.DB.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		var r OutboxRow
		if err := rows.Scan(&r.ID, &r.Subject, &r.Payload, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkPublished sets published_at=now() for the row.
func (s *PGOutboxStore) MarkPublished(ctx context.Context, id string) error {
	q := fmt.Sprintf(`UPDATE %s SET published_at = NOW() WHERE id = $1`, s.table())
	_, err := s.DB.ExecContext(ctx, q, id)
	return err
}

// OutboxMigrationSQL is the canonical migration applied by each service that
// adopts the transactional outbox pattern.
const OutboxMigrationSQL = `
CREATE TABLE IF NOT EXISTS outbox_events (
    id           TEXT PRIMARY KEY,
    subject      TEXT NOT NULL,
    payload      BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_outbox_events_unpublished
    ON outbox_events (created_at)
    WHERE published_at IS NULL;
`
