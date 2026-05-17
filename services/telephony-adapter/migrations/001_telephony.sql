-- Telephony adapter schema

CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    priority    INT NOT NULL DEFAULT 99,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS provider_credentials (
    provider_id TEXT PRIMARY KEY REFERENCES providers(id),
    auth_id     TEXT NOT NULL,
    auth_token  TEXT NOT NULL, -- stored encrypted at rest
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS call_sessions (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    provider_call_id TEXT,
    provider_name    TEXT,
    from_number      TEXT NOT NULL,
    to_number        TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'initiated',
    started_at       TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_call_sessions_tenant ON call_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_call_sessions_provider_call_id ON call_sessions(provider_call_id);

CREATE TABLE IF NOT EXISTS call_provider_events (
    id               TEXT PRIMARY KEY,
    session_id       TEXT REFERENCES call_sessions(id),
    provider_call_id TEXT NOT NULL,
    provider_name    TEXT NOT NULL,
    event_type       TEXT NOT NULL,
    raw_payload      JSONB,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_call_provider_events_session ON call_provider_events(session_id);

CREATE TABLE IF NOT EXISTS call_recordings (
    id               TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL REFERENCES call_sessions(id),
    provider_call_id TEXT NOT NULL,
    storage_key      TEXT NOT NULL,
    encrypted        BOOLEAN NOT NULL DEFAULT TRUE,
    size_bytes       BIGINT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS provider_health_checks (
    id          TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES providers(id),
    healthy     BOOLEAN NOT NULL,
    latency_ms  INT,
    checked_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_provider_health_checks_provider ON provider_health_checks(provider_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS provider_failures (
    id          TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES providers(id),
    reason      TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_provider_failures_provider ON provider_failures(provider_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS provider_routing_rules (
    id            TEXT PRIMARY KEY,
    provider_id   TEXT NOT NULL REFERENCES providers(id),
    region        TEXT NOT NULL DEFAULT '',
    priority      INT NOT NULL DEFAULT 99,
    max_fail_rate NUMERIC(4,3) NOT NULL DEFAULT 0.3,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_events (
    id            TEXT PRIMARY KEY,
    provider_name TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    raw_payload   JSONB,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published     BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key        TEXT PRIMARY KEY,
    result     BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
