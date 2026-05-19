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
