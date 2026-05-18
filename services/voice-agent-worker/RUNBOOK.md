# voice-agent-worker Runbook

Purpose: run the per-call VAD, STT, LLM, guardrail, TTS, recording, and action loop.

Dependencies: FreeSWITCH bridge, Redis session context, STT/LLM/guardrail/TTS routers, NATS, recorder.

Paging signals: call loop errors, no STT after speech, high barge-in latency, action publish failures.

Common fixes: inspect session context key, verify router URLs, switch model engines, replay 50-turn fixture, pause calls if audio path is broken.

Dashboards and logs: turn latency, barge-in events, transcript confidence, action events, call summaries.

Rollback: deploy previous worker and use demo mode to validate before real calls.
