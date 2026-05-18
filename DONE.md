# Build Complete

Capsy EVS has been built through Phase 25.

## What Was Built

- Multi-tenant auth, RBAC, lead import, lead identity, campaign scheduling, and workflows
- Telephony and FreeSWITCH bridge
- STT, LLM, guardrail, TTS, voice-agent worker, scoring, and eval harness
- WhatsApp adapter, templates, opt-out, webhooks, and billing metering
- Human handoff, notifications, site visits, analytics, reports, and ClickHouse sink
- Model switcher, internal admin API, internal admin UI
- Demo sandbox mode with deterministic seeding and mock providers
- Security audit harness and final documentation

## Run Locally

```bash
corepack pnpm install
uv sync
cd scripts && python seed_demo.py --dry-run
cd ../apps/dashboard && corepack pnpm exec next build
```

Run tests by service from each `RUNBOOK.md`. Some integration, race, scanner, chaos, and DR tests need the tools and infrastructure listed in `../HUMAN_TASKS.md`.

## Deploy

1. Build and push service images.
2. Apply infrastructure for DOKS, Postgres, Redis, NATS, Temporal, ClickHouse, Vault, object storage, and observability.
3. Deploy apps and services with Helm/Kustomize.
4. Run smoke tests for dashboard, internal admin, BFF, webhooks, voice path, model-config, and demo tenant.
5. Enable provider credentials in Vault per tenant.

## Rollback

- Roll back image tags in the release.
- Pause affected providers with internal admin kill switches.
- Restore database from PITR if data migration rollback is required.
- Use demo mode for smoke validation before re-enabling real outbound adapters.

## Compliance Snapshot

- DPDP, TRAI, RERA, webhook signature, idempotency, and RBAC checks have local deterministic tests.
- Phase 24 scanner and race commands are blocked locally by missing tools / 32-bit MinGW and are documented in `../HUMAN_TASKS.md`.

## Cost Snapshot

- Default LLM: `groq_llama`
- Default STT: `sarvam`
- Default TTS: `sarvam_bulbul`
- GPU models are optional and gated by model-config health.
- Demo tenant cost is always zero and uses mock providers only.
