CREATE TABLE IF NOT EXISTS site_visits (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  lead_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  proposed_slots JSONB NOT NULL DEFAULT '[]'::jsonb,
  confirmed_slot JSONB,
  sales_rep_id TEXT,
  state TEXT NOT NULL,
  notes TEXT,
  attendee_count INTEGER,
  loss_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS site_visit_events (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  visit_id UUID NOT NULL REFERENCES site_visits(id),
  type TEXT NOT NULL,
  from_state TEXT,
  to_state TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS site_visit_workflow_actions (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  visit_id UUID NOT NULL REFERENCES site_visits(id),
  type TEXT NOT NULL,
  due_at TIMESTAMPTZ NOT NULL,
  fired_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (visit_id, type, due_at)
);
