-- C4: Seed real provider defaults so routers pick the right engines out of the box.
-- Uses the pgkv backing table (kv_store) with namespace 'svc_model_config' and kind 'config'.
-- These rows are read by model-config Get("llm:default") etc.
--
-- The model_config_global view below is a convenience alias for direct SQL inspection.

CREATE TABLE IF NOT EXISTS model_config_global (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO model_config_global (key, value) VALUES
    ('llm:default', 'groq_llama'),
    ('stt:default', 'sarvam'),
    ('tts:default', 'sarvam_bulbul')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

-- Also write into the pgkv store so the running service picks them up.
INSERT INTO kv_store (namespace, kind, key, value)
VALUES
    ('svc_model_config', 'config', 'llm:default', '"groq_llama"'),
    ('svc_model_config', 'config', 'stt:default', '"sarvam"'),
    ('svc_model_config', 'config', 'tts:default', '"sarvam_bulbul"')
ON CONFLICT (namespace, kind, key) DO UPDATE SET value = EXCLUDED.value;
