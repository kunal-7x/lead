# Product Completion Track

This track is the new source of truth after Phase 25.

Goal: complete the full client-visible product flow in deterministic fake/demo mode first. Production credentials and real infrastructure come after this flow is proven end to end.

## Required Demo Flow

A tenant must be able to run this flow without real external side effects:

1. Client logs in to the dashboard.
2. Client uploads 10 leads from CSV/XLSX.
3. Import preview validates phones, maps columns, deduplicates contacts, and creates leads.
4. Client creates or selects a project with approved demo KB facts, FAQs, assets, RERA metadata, and disclaimers.
5. Client creates a campaign, attaches the 10 leads, and launches it.
6. Scheduler picks leads according to calling hours, retry rules, cost caps, and campaign limits.
7. Telephony adapter places fake/demo calls and emits realistic call status events.
8. Voice agent runs a full fake media conversation through STT -> LLM/RAG -> guardrail -> TTS.
9. Voice actions update lead status, score, transcript, summary, call timeline, handoff, callback, site visit, and WhatsApp intent.
10. WhatsApp adapter sends fake/demo template and service-window messages, receives scripted replies, and updates the inbox.
11. Dashboard shows live data for leads, calls, summaries, transcripts, WhatsApp messages, site visits, handoffs, reports, costs, and AI quality.
12. Internal admin can see provider/demo health, failed jobs, queue state, tenant/campaign controls, and audit events.
13. A single automated test proves the 10-lead journey from upload to dashboard evidence.

Production conversion must not start until this demo flow passes.

## Current Gaps

### 1. Dashboard-to-BFF API gap

The dashboard calls:

- `/api/v1/import/preview`
- `/api/v1/import/jobs`
- `/api/v1/leads`
- `/api/v1/leads/{id}`
- `/api/v1/leads/{id}/activities`
- `/api/v1/leads/{id}/status-history`

Status: partially closed in Phase 27.

Phase 27 added BFF proxy routes for lead-import and campaign APIs, plus safe empty lead timeline placeholders. Remaining BFF proxy gaps still exist for scheduler, telephony, WhatsApp, reports, billing, site-visit, handoff, and AI-quality APIs.

### 2. Missing client campaign UI

Status: closed in Phase 28.

Phase 28 added a client dashboard campaign page that can list campaigns, create a campaign, attach lead IDs, launch, pause/resume, and read campaign health through the BFF campaign proxy.

### 3. Runtime fake stores are isolated per service

Many services run with `store.NewFake()` at process start. That is acceptable for unit tests but not enough for the demo product unless all services share a demo scenario store or event bridge.

Impact: lead-import, campaign, scheduler, telephony, voice, WhatsApp, billing, reports, and dashboard do not share one coherent demo state.

### 4. Scheduler is not wired to imported leads and telephony

Status: closed for demo command dispatch in Phase 29.

Phase 29 added `/v1/scheduler/demo-dispatch`, which seeds deterministic demo leads, picks them, and posts call commands to telephony. Remaining work is to invoke this from campaign launch automatically and then persist downstream voice/WhatsApp events.

### 5. Telephony lacks product-level call orchestration API

Status: closed for demo call command in Phase 29.

Phase 29 added `POST /v1/calls`, which accepts tenant/campaign/lead/contact/project/phone/demo metadata, routes demo tenants to the mock provider, places the call, and stores the call session.

### 6. Voice worker uses fake publisher and fake turn store at runtime

Status: closed for demo runtime in Phase 30.

Phase 30 added JSONL-backed demo runtime implementations for event publishing and turn storage. Runtime voice artifacts are written under `VOICE_DEMO_DIR` or `.demo/voice-agent` by default. Unit-test fakes remain available only for tests.

### 7. Voice path is per-utterance batch, not streaming-optimized

The current loop buffers audio until silence, then calls STT, LLM, guardrail, and TTS. This is enough for deterministic demo proof, but not yet the final low-latency streaming target.

Impact: demo can prove behavior, but production latency work remains.

### 8. WhatsApp inbox is static UI data

The dashboard WhatsApp page uses local hardcoded thread/message arrays. The WhatsApp adapter has real/fake clients and webhook handling, but dashboard inbox data is not wired to adapter state.

Impact: post-call WhatsApp follow-up cannot be verified from the client UI.

### 9. WhatsApp conversational RAG is not wired

Meta sending and scripted demo replies exist, but the product path for:

`incoming WA reply -> RAG/brain -> safe response -> message send -> lead timeline`

is not complete.

Impact: WhatsApp can be a follow-up channel, but not yet a full AI conversation channel.

### 10. Lead timeline is incomplete

The dashboard expects activities and status history APIs, but lead-import only exposes basic lead create/list/get. Full timeline events from call, transcript, WhatsApp, handoff, site visit, billing, and AI quality are not unified.

Impact: client cannot inspect proof of work for every lead.

### 11. Reports dashboard is not backed by one demo event stream

Analytics and billing services exist, but demo calls/messages/site visits are not flowing into one reporting dataset that dashboard can query.

Impact: client reports cannot prove the 10-lead demo journey.

### 12. AI quality page is not connected to actual demo call outputs

Eval harness and AI quality UI exist, but demo call transcripts, brain outputs, guardrail decisions, and human corrections are not automatically pushed into review queues.

Impact: AI review is present as a module, not fully connected to the call journey.

### 13. Internal admin does not control the complete demo flow

Internal admin has provider and tenant controls, but it does not yet observe or manipulate the whole demo journey: demo reset, queue replay, failed event retry, and per-tenant pause/resume across all services.

Impact: support/debugging of the demo product is incomplete.

### 14. No single end-to-end demo test

Current tests are service/module focused. There is no one command that proves:

`seed/import 10 leads -> launch campaign -> process fake calls -> generate transcripts/summaries -> send fake WhatsApp -> create site visits/handoffs -> dashboard APIs return evidence`

Impact: we cannot honestly call the mock/demo product complete yet.

## Build Order

### Phase 26: Demo product gap close plan and failing acceptance harness

- Add a deterministic 10-lead demo scenario definition.
- Add one acceptance test command that describes the required journey.
- Keep it focused on fake/demo mode.

### Phase 27: BFF product API proxy

- Proxy dashboard routes to lead-import, campaign, scheduler, telephony, WhatsApp, reports, billing, site-visit, handoff, and AI-quality services.
- Add tenant header propagation.
- Add dashboard API tests for lead import/list/detail.

### Phase 28: Shared demo scenario state

- Replace isolated fake runtime state with a deterministic demo state layer for the full journey.
- Keep per-service fakes for unit tests.
- Add reset endpoint and seeded 10-lead data.

### Phase 29: Campaign-to-call demo orchestration

- Add client campaign UI.
- Wire imported leads to campaign launch.
- Wire scheduler demo pick loop to telephony fake calls.
- Record call sessions and state transitions.

### Phase 30: Voice event persistence in demo mode

- Replace runtime `FakePublisher`/`FakeTurnStore` with demo NATS/demo store implementations.
- Persist transcript, turns, brain output, guardrail result, TTS tier/cache status, and call summary.

### Phase 31: WhatsApp demo conversation loop

- Wire call WhatsApp actions to WhatsApp adapter demo client.
- Replace static inbox with API-backed demo threads.
- Add scripted reply -> brain/RAG -> safe response -> timeline update.

### Phase 32: Dashboard proof surfaces

- Show live demo metrics, lead timelines, call summaries, transcripts, WhatsApp messages, site visits, handoffs, costs, and AI quality review.
- Add Playwright coverage for client-visible proof.

### Phase 33: Full mock/demo E2E pass

- One command proves the complete 10-lead journey.
- Demo mode remains side-effect safe.
- Production mode still not enabled.

### Phase 34: Production conversion readiness gate

- Add production-mode startup refusal for runtime fake stores/providers.
- List every remaining fake/demo-only path.
- Only after this phase should real credentials be wired.

## Exit Criteria Before Production Conversion

The mock/demo product is complete only when:

- A clean checkout can seed or upload 10 demo leads.
- Dashboard can launch a campaign against those leads.
- All 10 leads receive deterministic fake call outcomes.
- At least one lead gets hot handoff, one site visit, one callback, one opt-out, and one no-answer retry.
- Call transcript, AI summary, lead score, status, and next action are visible on lead detail.
- WhatsApp follow-up and replies are visible in inbox and timeline.
- Reports show calls, connect rate, hot/warm/cold counts, site visits, WhatsApp delivery, and cost.
- Internal admin can reset demo data and inspect provider/demo health.
- The full acceptance test passes locally.

