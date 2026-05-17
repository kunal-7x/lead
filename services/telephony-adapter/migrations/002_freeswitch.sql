-- Phase 10: FreeSWITCH + Kamailio tables

-- Registered FreeSWITCH nodes with health status.
-- Kamailio's dispatcher.list seeds from this table on startup.
CREATE TABLE IF NOT EXISTS freeswitch_instances (
    id             TEXT PRIMARY KEY,
    host           TEXT NOT NULL,
    sip_port       INTEGER NOT NULL DEFAULT 5060,
    esl_port       INTEGER NOT NULL DEFAULT 8021,
    healthy        BOOLEAN NOT NULL DEFAULT TRUE,
    last_checked   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Queue for WAV recordings pending upload to DO Spaces.
-- freeswitch-bridge worker drains this queue, retrying up to 3 times.
CREATE TABLE IF NOT EXISTS recording_upload_queue (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    session_id     TEXT NOT NULL,
    local_path     TEXT NOT NULL,               -- /tmp/recordings/{tenant_id}_{session_id}.wav
    spaces_key     TEXT,                        -- set on success
    status         TEXT NOT NULL DEFAULT 'pending', -- pending | uploaded | failed
    attempts       INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- SIP trunk configuration per environment.
-- Jio: IP-based auth (no username/password), port 5060, PCMU/PCMA.
-- Airtel: fallback trunk.
-- Disabled at startup; enabled via ESL when credentials present in env.
CREATE TABLE IF NOT EXISTS sip_trunks (
    id             TEXT PRIMARY KEY,
    provider       TEXT NOT NULL,              -- 'jio' | 'airtel' | 'plivo'
    environment    TEXT NOT NULL DEFAULT 'production',
    gateway_host   TEXT NOT NULL,
    gateway_port   INTEGER NOT NULL DEFAULT 5060,
    auth_type      TEXT NOT NULL DEFAULT 'ip', -- 'ip' | 'credentials'
    username       TEXT,
    enabled        BOOLEAN NOT NULL DEFAULT FALSE,
    codec          TEXT NOT NULL DEFAULT 'PCMU,PCMA',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed Jio and Airtel trunk rows (disabled until real credentials loaded from env).
INSERT INTO sip_trunks (id, provider, gateway_host, auth_type, enabled)
VALUES
    ('trunk-jio',    'jio',    'jio-sip.example.com',    'ip',          FALSE),
    ('trunk-airtel', 'airtel', 'airtel-sip.example.com', 'credentials', FALSE)
ON CONFLICT (id) DO NOTHING;
