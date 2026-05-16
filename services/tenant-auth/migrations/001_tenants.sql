-- +goose Up
CREATE TABLE tenants (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    region     TEXT        NOT NULL DEFAULT 'us-east-1',
    status     TEXT        NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active','suspended','deleted')),
    metadata   JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tenants_status ON tenants(status);

-- +goose Down
DROP TABLE IF EXISTS tenants;
