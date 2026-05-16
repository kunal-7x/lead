-- +goose Up
CREATE TABLE api_keys (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES tenant_users(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    key_hash   TEXT        NOT NULL UNIQUE,
    key_prefix TEXT        NOT NULL,
    revoked    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX idx_api_keys_hash   ON api_keys(key_hash) WHERE NOT revoked;

-- +goose Down
DROP TABLE IF EXISTS api_keys;
