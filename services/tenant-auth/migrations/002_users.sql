-- +goose Up
CREATE TABLE tenant_users (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email              TEXT        NOT NULL,
    display_name       TEXT        NOT NULL DEFAULT '',
    password_hash      TEXT        NOT NULL,
    totp_secret        TEXT,
    totp_enabled       BOOLEAN     NOT NULL DEFAULT FALSE,
    refresh_token_hash TEXT,
    status             TEXT        NOT NULL DEFAULT 'active'
                                   CHECK (status IN ('active','suspended','deleted')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, email)
);

CREATE INDEX idx_tenant_users_tenant ON tenant_users(tenant_id);
CREATE INDEX idx_tenant_users_refresh ON tenant_users(refresh_token_hash) WHERE refresh_token_hash IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS tenant_users;
