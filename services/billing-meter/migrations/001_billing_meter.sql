CREATE TABLE IF NOT EXISTS usage_events (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  campaign_id TEXT,
  type TEXT NOT NULL,
  quantity BIGINT NOT NULL,
  unit TEXT NOT NULL,
  provider TEXT,
  category TEXT,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  occurred_at TIMESTAMPTZ NOT NULL,
  idempotency_key TEXT UNIQUE
);

CREATE TABLE IF NOT EXISTS cost_events (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  campaign_id TEXT,
  usage_event_id UUID NOT NULL REFERENCES usage_events(id),
  type TEXT NOT NULL,
  quantity BIGINT NOT NULL,
  unit TEXT NOT NULL,
  unit_cost_inr NUMERIC(12, 6) NOT NULL,
  total_inr NUMERIC(12, 4) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_usage (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  type TEXT NOT NULL,
  quantity BIGINT NOT NULL,
  unit TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS usage_meters (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  usage_count BIGINT NOT NULL DEFAULT 0,
  total_cost_inr NUMERIC(12, 4) NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS client_cost_summaries (
  tenant_id TEXT PRIMARY KEY,
  usage_count BIGINT NOT NULL DEFAULT 0,
  total_cost_inr NUMERIC(12, 4) NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS campaign_cost_summaries (
  campaign_id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  usage_count BIGINT NOT NULL DEFAULT 0,
  total_cost_inr NUMERIC(12, 4) NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS billing_accounts (
  tenant_id TEXT PRIMARY KEY,
  plan_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS plans (
  id TEXT PRIMARY KEY,
  monthly_inr NUMERIC(12, 2) NOT NULL
);

CREATE TABLE IF NOT EXISTS credits_ledger (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  amount_inr NUMERIC(12, 2) NOT NULL,
  reason TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
