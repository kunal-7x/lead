# Campaign Run System — Operator Runbook

End-to-end outbound campaign: create (manual or AI-assist) → import leads (CSV) →
attach → **Run** → dispatcher dials in concurrent batches under tenant limits,
with disposition retries + TRAI compliance, while the voice AI is grounded in the
campaign's rich context. Built across phases A–F (see git log `Phase A..F`).

## Components & flow
```
Dashboard /campaigns ──BFF──> campaign svc ──launch──> NATS campaign.launched
                                                            │
scheduler DISPATCHER (internal/dispatcher) <───────────────┘
  load campaign(context+limits) + leads + phones(lead-import)
  → seed durable pgkv dial_queue
  → 2s ticker: suppression scrub → TRAI window → rate limit → POST telephony /v1/calls
        (callback_url + machine_detection); 503 = no slot (telephony cap, default 3)
  → on 2xx: write Redis call:session:{provider_call_id} = campaign_context  ← grounds AI
  → consume call.completed/call.failed → disposition → retry (busy 20m / no-ans 180m /
        fail 30m; max RetryMax) or done/exhausted
voice-agent-worker reads call:session:{CallUUID} → llm-router injects campaign_context
```

## Required env (per service) for a live run
- **telephony-adapter** (`:8108`): `VOBIZ_AUTH_ID`,`VOBIZ_AUTH_TOKEN`,`VOBIZ_BASE_URL`,
  `VOBIZ_MAX_CONCURRENT_CALLS=3`, `CALLING_PROVIDER=vobiz`, `REDIS_URL`, `NATS_URL`,
  `DATABASE_URL`, `PUBLIC_WEBHOOK_BASE_URL=<ngrok https url>`.
- **scheduler**: `NATS_URL`, `REDIS_URL`, `DATABASE_URL`, `CAMPAIGN_URL`,
  `LEAD_IMPORT_URL`, `TELEPHONY_URL=http://localhost:8108`, `FROM_NUMBER=<vobiz DID>`,
  `PUBLIC_WEBHOOK_BASE_URL=<same ngrok url>`.
- **campaign** (`:8112`): `DATABASE_URL`, `NATS_URL`, `LLM_ROUTER_URL=http://localhost:8111`.
- **llm-router** (`:8111`): `LLM_API_KEY`,`LLM_BASE_URL`,`LLM_MODEL` (extractor + persona).
- **voice-agent-worker**: `REDIS_URL`, `STT_ROUTER_URL`, `LLM_ROUTER_URL`, `TTS_ROUTER_URL`.
- **bff**: `CAMPAIGN_URL`, `SCHEDULER_URL=http://localhost:8107`, `LEAD_IMPORT_URL`, auth env.

## Run locally (Windows)
1. Start Docker Desktop, then infra: `docker compose -f infra/docker-compose.dev.yml up -d`
   (postgres 5432, redis 6379, nats 4222, temporal, livekit, vault).
2. Start each Go service: `go run ./cmd/server` in its dir (telephony, scheduler, campaign, bff).
   Start Python: `uv run uvicorn ...` (llm-router, voice-agent-worker) — see each service RUNBOOK.
3. Expose telephony for Vobiz webhooks: `ngrok http 8108` → set `PUBLIC_WEBHOOK_BASE_URL`
   to the https URL for telephony AND scheduler (answer/stream + callback).

## Live test (3 real numbers in all_credential.md)
1. Smoke one call first: `scripts/call.cmd <number>` → confirm AI answers grounded.
2. Dashboard: create a campaign (or paste a brief → Extract & Prefill), set Pacing
   (concurrent 3 / hourly 50 / daily 500 / window 10–19 IST).
3. CSV-import the 3 numbers as leads (/leads/import) → attach to the campaign.
4. Click **Run Campaign**. Watch the live progress panel (placed/connected/in_flight/
   no_answer_retry/failed/suppressed/remaining, polled every 2s).
5. Expect: all 3 dial concurrently (cap 3); a 4th would wait for a slot. No-answer
   reschedules per policy. Confirm rings/pickups per number.

## Known follow-ups (HUMAN_TASKS)
- **Cost cap**: `campaign:cost:day:{id}` counter is checked but not yet fed (cost cap
  defaults to unlimited). Wire a billing.usage consumer to increment it.
- **NCPR/DND feed**: suppression store is wired but seeded manually; needs a TRAI NCPR
  scrub source (cron).
- **AMD**: `machine_detection` is passed to telephony; Vobiz support is best-effort.
- llm-router extractor JSON mode: add a Gemini path / parse-from-text fallback.
