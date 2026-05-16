-- +goose Up
CREATE TABLE audit_logs (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    actor_id     TEXT        NOT NULL DEFAULT '',
    actor_email  TEXT        NOT NULL DEFAULT '',
    action       TEXT        NOT NULL,
    resource     TEXT        NOT NULL DEFAULT '',
    resource_id  TEXT        NOT NULL DEFAULT '',
    before_state JSONB,
    after_state  JSONB,
    ip_address   TEXT        NOT NULL DEFAULT '',
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only: no UPDATE or DELETE allowed (enforced by RLS in next migration).
CREATE INDEX idx_audit_logs_tenant_time ON audit_logs(tenant_id, occurred_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_logs;
