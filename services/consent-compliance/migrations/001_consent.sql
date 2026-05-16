-- consent_ledger: append-only; UPDATE/DELETE blocked by trigger
CREATE TABLE IF NOT EXISTS consent_ledger (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id        UUID        NOT NULL,
    basis          TEXT        NOT NULL,
    source         TEXT        NOT NULL DEFAULT '',
    notice_version TEXT        NOT NULL DEFAULT '',
    evidence_url   TEXT        NOT NULL DEFAULT '',
    pii_redacted   BOOLEAN     NOT NULL DEFAULT FALSE,
    redacted_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Enforce append-only on consent_ledger.
CREATE OR REPLACE FUNCTION consent_ledger_append_only()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'consent_ledger is append-only: % not permitted', TG_OP;
END;
$$;

DROP TRIGGER IF EXISTS trg_consent_ledger_append_only ON consent_ledger;
CREATE TRIGGER trg_consent_ledger_append_only
    BEFORE UPDATE OR DELETE ON consent_ledger
    FOR EACH ROW EXECUTE FUNCTION consent_ledger_append_only();

-- suppression_list: phones that must never be contacted
CREATE TABLE IF NOT EXISTS suppression_list (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    phone      TEXT        NOT NULL UNIQUE,
    reason     TEXT        NOT NULL,
    ticket_id  TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- whatsapp_opt_outs: per-lead opt-outs; inserts also append to consent_ledger
CREATE TABLE IF NOT EXISTS whatsapp_opt_outs (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id    UUID        NOT NULL,
    channel    TEXT        NOT NULL DEFAULT 'whatsapp',
    reason     TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (lead_id, channel)
);
