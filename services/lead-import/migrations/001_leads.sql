-- Lead import tables

CREATE TABLE IF NOT EXISTS lead_sources (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL, -- 'csv', 'api', 'fb_lead_ads', 'google_lead_forms'
    config      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS contacts (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    phone_e164  TEXT NOT NULL,
    email       TEXT,
    name        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS contacts_tenant_phone_uidx ON contacts (tenant_id, phone_e164);

CREATE TABLE IF NOT EXISTS leads (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    contact_id  UUID NOT NULL REFERENCES contacts(id),
    source_id   UUID REFERENCES lead_sources(id),
    status      TEXT NOT NULL DEFAULT 'new',
    score       INT  NOT NULL DEFAULT 0,
    assigned_to UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS leads_tenant_idx      ON leads (tenant_id);
CREATE INDEX IF NOT EXISTS leads_contact_idx     ON leads (contact_id);
CREATE INDEX IF NOT EXISTS leads_status_idx      ON leads (tenant_id, status);
CREATE INDEX IF NOT EXISTS leads_assigned_idx    ON leads (tenant_id, assigned_to);

CREATE TABLE IF NOT EXISTS lead_field_values (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id  UUID NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
    field    TEXT NOT NULL,
    value    TEXT,
    UNIQUE (lead_id, field)
);

CREATE TABLE IF NOT EXISTS lead_status_history (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id    UUID NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
    old_status TEXT,
    new_status TEXT NOT NULL,
    actor_id   UUID,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lead_activities (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id    UUID NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    payload    JSONB,
    actor_id   UUID,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS activities_lead_idx ON lead_activities (lead_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS lead_source_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id     UUID NOT NULL REFERENCES lead_sources(id),
    idempotency_key TEXT UNIQUE,
    payload       JSONB NOT NULL,
    processed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lead_import_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    source_id       UUID REFERENCES lead_sources(id),
    file_url        TEXT,
    mapping         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending', -- pending, running, done, failed
    total_rows      INT  NOT NULL DEFAULT 0,
    imported_rows   INT  NOT NULL DEFAULT 0,
    error_rows      INT  NOT NULL DEFAULT 0,
    errors          JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key        TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
