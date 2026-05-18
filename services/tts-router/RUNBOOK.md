# tts-router Runbook

Purpose: synthesize 8kHz L16 PCM speech through cache, Sarvam, Indic engines, Kokoro, or ElevenLabs.

Dependencies: model-config Redis keys, TTS cache, provider credentials, voice profiles.

Paging signals: first chunk latency, cache miss spike, engine offline, audio format mismatch.

Common fixes: warm cache, switch engine to Sarvam Bulbul or Kokoro, verify voice id, inspect resampling.

Dashboards and logs: engine used, cache hit rate, first chunk latency, output format checks.

Rollback: set global engine to `sarvam_bulbul` and deploy previous image.
