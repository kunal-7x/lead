CREATE TABLE IF NOT EXISTS whatsapp_templates (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  language TEXT NOT NULL,
  category TEXT NOT NULL CHECK (category IN ('marketing', 'utility', 'authentication')),
  body TEXT NOT NULL,
  status TEXT NOT NULL,
  meta_name TEXT NOT NULL,
  variables JSONB NOT NULL DEFAULT '[]'::jsonb,
  remote_id TEXT,
  remote_note TEXT,
  synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name, language)
);

CREATE TABLE IF NOT EXISTS whatsapp_threads (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  lead_id TEXT NOT NULL,
  phone TEXT NOT NULL,
  last_inbound_at TIMESTAMPTZ,
  service_window_until TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, lead_id),
  UNIQUE (tenant_id, phone)
);

CREATE TABLE IF NOT EXISTS whatsapp_messages (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  thread_id UUID NOT NULL REFERENCES whatsapp_threads(id),
  lead_id TEXT NOT NULL,
  phone TEXT NOT NULL,
  direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
  kind TEXT NOT NULL CHECK (kind IN ('text', 'template', 'flow')),
  body TEXT NOT NULL DEFAULT '',
  template_id UUID REFERENCES whatsapp_templates(id),
  flow_id TEXT,
  attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL,
  meta_message_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS whatsapp_webhook_events (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL UNIQUE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS whatsapp_opt_outs (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  lead_id TEXT NOT NULL,
  phone TEXT NOT NULL,
  channel TEXT NOT NULL DEFAULT 'whatsapp',
  reason TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, lead_id, channel)
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
  key TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS usage_events (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  lead_id TEXT NOT NULL,
  message_id UUID,
  event_type TEXT NOT NULL,
  quantity INTEGER NOT NULL DEFAULT 1,
  unit_cost_inr NUMERIC(10, 4) NOT NULL,
  total_cost_inr NUMERIC(10, 4) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
