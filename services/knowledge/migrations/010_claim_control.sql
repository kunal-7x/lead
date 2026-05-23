CREATE TABLE IF NOT EXISTS project_claims (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,
    project_id UUID REFERENCES projects(id),
    claim_type TEXT NOT NULL CHECK (claim_type IN (
        'price','discount','possession','rera','loan','offer','roi','appreciation','urgency','spec','other'
    )),
    status TEXT NOT NULL CHECK (status IN ('allowed','forbidden','needs_human_approval')),
    pattern TEXT NOT NULL,
    pattern_kind TEXT NOT NULL CHECK (pattern_kind IN ('regex','phrase','semantic')),
    replacement TEXT,
    valid_from TIMESTAMPTZ,
    valid_until TIMESTAMPTZ,
    approver_user_id UUID,
    source TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS project_claims_project_status_idx
    ON project_claims(project_id, status, claim_type);

CREATE TABLE IF NOT EXISTS claim_violations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,
    project_id UUID,
    call_id UUID,
    lead_id UUID,
    channel TEXT NOT NULL CHECK (channel IN ('voice','whatsapp')),
    attempted_text TEXT NOT NULL,
    matched_claim_id UUID,
    claim_type TEXT,
    action_taken TEXT NOT NULL CHECK (action_taken IN ('blocked','rewritten','allowed_with_warning')),
    reason TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS claim_violations_project_time_idx
    ON claim_violations(project_id, occurred_at DESC);
