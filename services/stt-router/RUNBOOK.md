# stt-router Runbook

Purpose: route speech-to-text requests across Sarvam, IndicConformer, Groq Whisper, and faster-whisper.

Dependencies: model-config Redis keys, provider credentials, self-hosted endpoints, voice-agent-worker.

Paging signals: STT timeout, confidence drop, engine offline, fallback rate spike.

Common fixes: switch engine to Sarvam or Groq, verify endpoint env vars, restart self-hosted sidecar, inspect audio format.

Dashboards and logs: engine latency, confidence, fallback count, language mix.

Rollback: set global engine to `sarvam` and deploy previous router image.
