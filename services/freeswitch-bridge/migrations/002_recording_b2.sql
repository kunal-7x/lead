-- Phase C8: WORM archive columns for B2 + presigned playback URLs.

ALTER TABLE recording_upload_queue
    ADD COLUMN IF NOT EXISTS b2_key        TEXT,
    ADD COLUMN IF NOT EXISTS archived_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS playback_url  TEXT;

CREATE INDEX IF NOT EXISTS recording_upload_queue_status_idx
    ON recording_upload_queue (status, created_at);

CREATE INDEX IF NOT EXISTS recording_upload_queue_tenant_idx
    ON recording_upload_queue (tenant_id, session_id);
