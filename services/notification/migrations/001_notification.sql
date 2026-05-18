CREATE TABLE IF NOT EXISTS notifications (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  channel TEXT NOT NULL,
  recipient TEXT NOT NULL,
  template TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL,
  error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notification_subscriptions (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  event_types TEXT[] NOT NULL,
  channels TEXT[] NOT NULL,
  recipients TEXT[] NOT NULL,
  template TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notification_retry_tasks (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  notification_id UUID NOT NULL REFERENCES notifications(id),
  channel TEXT NOT NULL,
  recipient TEXT NOT NULL,
  run_after TIMESTAMPTZ NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 1,
  last_error TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
