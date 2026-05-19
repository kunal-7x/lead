-- C3: transactional outbox for NATS JetStream publishing
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
