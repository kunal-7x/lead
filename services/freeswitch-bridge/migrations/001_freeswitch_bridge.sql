CREATE TABLE IF NOT EXISTS freeswitch_instances (
    id           TEXT        PRIMARY KEY,
    host         TEXT        NOT NULL,
    sip_port     INTEGER     NOT NULL DEFAULT 5060,
    esl_port     INTEGER     NOT NULL DEFAULT 8021,
    healthy      BOOLEAN     NOT NULL DEFAULT TRUE,
    last_checked TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS recording_upload_queue (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    session_id  TEXT        NOT NULL,
    local_path   TEXT        NOT NULL DEFAULT '',
    spaces_key   TEXT,
    status      TEXT        NOT NULL DEFAULT 'pending',
    attempts    INTEGER     NOT NULL DEFAULT 0,
    last_error  TEXT,
    size_bytes  BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS c1_store_records (
    kind       TEXT        NOT NULL,
    key        TEXT        NOT NULL,
    tenant_id  TEXT        NOT NULL DEFAULT '',
    data       JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, key)
);

CREATE INDEX IF NOT EXISTS c1_store_records_tenant_idx
    ON c1_store_records (kind, tenant_id, created_at);
