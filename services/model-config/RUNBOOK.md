# model-config Runbook

Purpose: super-admin model switcher for global and tenant LLM/STT/TTS routing.

Dependencies: Redis-compatible model keys, provider endpoint env vars, internal admin auth.

Paging signals: invalid model id, GPU endpoint offline, tenant override drift, health endpoint failures.

Common fixes: restore defaults, remove tenant override, verify endpoint env vars, use managed API fallback.

Dashboards and logs: current model map, health status, GPU warnings, admin changes.

Rollback: set defaults to `groq_llama`, `sarvam`, and `sarvam_bulbul`; deploy previous image.
