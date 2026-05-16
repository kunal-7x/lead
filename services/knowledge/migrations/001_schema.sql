CREATE EXTENSION IF NOT EXISTS pgvector;

CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    name TEXT NOT NULL,
    rera_number TEXT,
    brochure_asset_id UUID,
    price_sheet_asset_id UUID,
    active_version_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_kb_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id),
    version_number INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft', -- draft | pending_approval | approved | published | rejected
    embedding_model TEXT NOT NULL DEFAULT 'BAAI/bge-m3',
    submitted_by UUID,
    reviewed_by UUID,
    review_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_facts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES project_kb_versions(id),
    content TEXT NOT NULL,
    metadata JSONB,
    embedding VECTOR(1536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_faqs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES project_kb_versions(id),
    question TEXT NOT NULL,
    answer TEXT NOT NULL,
    embedding VECTOR(1536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES project_kb_versions(id),
    name TEXT NOT NULL,
    asset_type TEXT NOT NULL, -- brochure | price_sheet | floor_plan | image | other
    storage_key TEXT,
    status TEXT NOT NULL DEFAULT 'pending', -- pending | scanning | ready | rejected
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_inventory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES project_kb_versions(id),
    unit_type TEXT NOT NULL,
    total_units INT NOT NULL DEFAULT 0,
    available_units INT NOT NULL DEFAULT 0,
    price_min NUMERIC,
    price_max NUMERIC,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_offers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES project_kb_versions(id),
    title TEXT NOT NULL,
    description TEXT,
    valid_until DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_disclaimers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES project_kb_versions(id),
    text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE kb_approval_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES project_kb_versions(id),
    action TEXT NOT NULL, -- submitted | approved | rejected | published
    actor_id UUID NOT NULL,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE pronunciation_dictionary (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    term TEXT NOT NULL,
    ipa TEXT,
    phonetic TEXT,
    lang TEXT NOT NULL DEFAULT 'en',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, term, lang)
);
