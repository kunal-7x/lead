CREATE TABLE IF NOT EXISTS handoffs (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  lead_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  summary TEXT NOT NULL,
  scoring_snapshot_id TEXT NOT NULL,
  assigned_user_id TEXT,
  manager_user_id TEXT,
  status TEXT NOT NULL,
  sla_deadline TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  acknowledged_at TIMESTAMPTZ,
  escalated_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS lead_assignments (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  lead_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  handoff_id UUID NOT NULL REFERENCES handoffs(id),
  assigned_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  handoff_id UUID NOT NULL REFERENCES handoffs(id),
  lead_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  due_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  closed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS task_events (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  task_id UUID,
  handoff_id UUID NOT NULL REFERENCES handoffs(id),
  type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS salesperson_sla_events (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  handoff_id UUID NOT NULL REFERENCES handoffs(id),
  stage TEXT NOT NULL,
  type TEXT NOT NULL,
  user_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
