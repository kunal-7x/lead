-- Lead identity tables

CREATE TABLE IF NOT EXISTS contacts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    phone_e164  TEXT NOT NULL,
    email       TEXT,
    name        TEXT,
    merged_into UUID REFERENCES contacts(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS contacts_tenant_phone_uidx
    ON contacts (tenant_id, phone_e164)
    WHERE merged_into IS NULL;

CREATE TABLE IF NOT EXISTS lead_identity_map (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL,
    phone_e164    TEXT,
    email         TEXT,
    contact_id    UUID NOT NULL REFERENCES contacts(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idmap_tenant_phone_idx ON lead_identity_map (tenant_id, phone_e164);
CREATE INDEX IF NOT EXISTS idmap_tenant_email_idx ON lead_identity_map (tenant_id, email);

CREATE TABLE IF NOT EXISTS contact_merge_audit (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL,
    primary_id    UUID NOT NULL,
    duplicate_id  UUID NOT NULL,
    actor_id      UUID,
    merged_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
