# Pipecat Runtime — Build Spec (the builder's bible)

This replaces ONLY the broken hand-rolled audio loop in `voice-agent-worker` with a
Pipecat pipeline, **over the existing Vobiz WebSocket** (no LiveKit, no SIP, no new ports).
Build into a NEW parallel package `voice_agent/pipecat_runtime/` behind a feature flag.
The old worker stays runnable for rollback.

## Architecture — "targeted hybrid" (what we cut vs keep)

PIPECAT OWNS (the broken/fragile parts we're fixing):
- Vobiz WebSocket transport + serializer (vendor `pipecat-vobiz`).
- VAD (Silero) + turn detection (Smart Turn v3) + interruption handling → replaces the
  energy-delta/probe-STT barge-in storm.
- **STT socket** — Pipecat `SarvamSTTService` calling Sarvam directly (keepalive WS; bypasses `stt-router`).
- **TTS socket** — Pipecat `SarvamTTSService` / `ElevenLabsTTSService` calling providers directly
  (keepalive WS; **bypasses the broken `tts-router` Sarvam streaming socket — THIS is the dead-air fix**).

WE KEEP UNTOUCHED (the brain + product):
- **`llm-router`** (`LLM_ROUTER_URL`, default `http://llm-router:8111`) holds the persona/system
  prompt, the BrainOutput JSON schema, slot collection, guardrail, and the **3-key Groq rotation**.
  Pipecat's LLM stage is a CUSTOM processor that calls llm-router — do NOT move the brain out.
- CSO/RSP engine (`conversation_state.py`), actions→NATS (`actions.py`), `prosody.ProsodyShaper`,
  `models.py`, `voice_gender.py` — reused as libraries.
- Redis `call:session:{id}` session handoff and all NATS events — reproduced exactly.
- telephony-adapter, scheduler, campaign, bff, dashboard — zero changes.

## Verified Pipecat APIs (mid-2026; pin pipecat-ai==1.3.*)
- Serializer: `from pipecat.serializers.vobiz import VobizFrameSerializer` (pkg `pipecat-vobiz>=0.0.3`).
  Ctor `(stream_id, call_id=None, auth_id=None, auth_token=None, params=...)`. Emits the exact
  `{event:playAudio}` / `{event:clearAudio}` Vobiz shapes; parses inbound `{event:media}` base64 µ-law.
  Fallback if needed: `pipecat.serializers.plivo.PlivoFrameSerializer` (identical shapes).
- Transport: `from pipecat.transports.websocket.fastapi import FastAPIWebsocketTransport, FastAPIWebsocketParams`.
  Params: `serializer=VobizFrameSerializer(...)`, `vad_analyzer=SileroVADAnalyzer()`,
  `turn_analyzer=LocalSmartTurnAnalyzerV3()`, `audio_in_enabled=True`, `audio_out_enabled=True`,
  `add_wav_header=False`, `session_timeout=...`. (v1.0 removed old `vad_enabled` flags — analyzer object only.)
- VAD: `from pipecat.audio.vad.silero import SileroVADAnalyzer`.
- Turn: `from pipecat.audio.turn.smart_turn.local_smart_turn_v3 import LocalSmartTurnAnalyzerV3` (bundled ONNX, CPU, Hindi-ok).
- STT: `from pipecat.services.sarvam.stt import SarvamSTTService` (streaming WS, keepalive; model `saaras:v3`, lang `hi-IN`/auto).
- TTS: `from pipecat.services.sarvam.tts import SarvamTTSService` (model `bulbul:v3`, speaker e.g. `rahul`);
  `from pipecat.services.elevenlabs.tts import ElevenLabsTTSService` (8kHz native; runtime `TTSUpdateSettingsFrame`).
- Pipeline: `from pipecat.pipeline.pipeline import Pipeline`; `from pipecat.pipeline.task import PipelineTask, PipelineParams`;
  `from pipecat.pipeline.runner import PipelineRunner`. Params: `allow_interruptions=True`,
  `interruption_strategies=[MinWordsInterruptionStrategy(min_words=2)]`, `enable_metrics=True`.
- Custom logic: subclass `from pipecat.processors.frame_processor import FrameProcessor`.
- Frames: `TextFrame`, `TranscriptionFrame`/`InterimTranscriptionFrame`, `LLMFullResponseStartFrame`/`EndFrame`,
  `StartInterruptionFrame`, `EndFrame`, `TTSAudioRawFrame` (`from pipecat.frames.frames import ...`).
- **PIN `sarvamai>=0.1.25`** (older broke saaras/bulbul v3 prompt/mode).

## File layout to create (under `voice_agent/pipecat_runtime/`)
- `__init__.py`
- `config.py` — read env (see ENV table below) into a typed settings object; provider selection helper.
- `app.py` — FastAPI app: `GET /pipecat/answer` (returns the Vobiz `<Stream>` XML) + `WS /pipecat/ws/{call_id}`
  (accept WS, read `start`, load Redis session, build serializer+transport+pipeline, run `PipelineTask`).
  uvicorn entry; bind `127.0.0.1`, port from `PIPECAT_PORT` (default 8140 — NEW port, behind nginx).
- `pipeline.py` — `build_pipeline(ctx, websocket) -> (PipelineTask, runner)`. Assembles:
  `transport.input() → SarvamSTTService → CsoProcessor → LlmRouterProcessor → tts(per ctx) → transport.output()`
  plus an `ActionsProcessor` tapping BrainOutput. Wire VAD/turn/interruption params.
- `llm_router_service.py` — `LlmRouterProcessor(FrameProcessor)`: on user transcription, maintain
  `dialog_history` (window `LLM_HISTORY_MAX_MSGS=12`), POST the EXACT llm-router payload (below) to
  `LLM_ROUTER_URL/v1/llm/generate/stream_text` (SSE → push `TextFrame` tokens, bracket with
  `LLMFullResponseStartFrame`/`EndFrame`), and fire a PARALLEL POST to `/v1/llm/generate` → parse
  `body["brain"]` into a `BrainOutput`, emit it as a custom frame for `ActionsProcessor`. Apply
  `ProsodyShaper.shape()` to text before it reaches TTS (sanitises markdown/symbols). Handle
  `StartInterruptionFrame` → cancel in-flight SSE.
- `cso_processor.py` — `CsoProcessor(FrameProcessor)`: holds a `ConversationStateEngine`; on user
  transcription fire `engine.update(dialog_history, user_turn)` as a background task; expose
  `engine.directive()` so `LlmRouterProcessor` injects it as `system_prompt_suffix`. (Simplest: put the
  engine on shared session state both processors read; or have CsoProcessor stash the directive on a
  context object.) Also surface the `[LOW_CONF_TURN]` suffix logic if barge-in confidence is low.
- `actions_processor.py` — `ActionsProcessor(FrameProcessor)`: on each `BrainOutput`, replicate
  `actions.handle_actions` → publish the NATS subjects below. Also record `call.turn.recorded` and, at
  call end, `call.completed`. Reuse the existing NATS publisher util.
- `xml.py` — build the Vobiz Answer `<Stream bidirectional="true" contentType="audio/x-mulaw;rate=8000">wss://.../pipecat/ws/{id}</Stream>` (mirror telephony-adapter's existing shape).
- `requirements-pipecat.txt` — `pipecat-ai[silero,sarvam,elevenlabs,websocket]==1.3.*`, `pipecat-vobiz>=0.0.3`,
  `sarvamai>=0.1.25`, plus existing fastapi/uvicorn/redis/nats-py/httpx/numpy. (Separate file so the OLD
  worker isn't forced to install the heavy pipecat stack.)
- `tests/` — pytest, all provider/network mocked: serializer round-trip (µ-law↔frame), `LlmRouterProcessor`
  emits TextFrames from a fake SSE + builds the exact payload, `CsoProcessor` injects directive,
  `ActionsProcessor` publishes the right NATS subjects per BrainOutput flag, provider-selection helper.

## llm-router payload contract (MUST match exactly — this is the brain interface)
POST `LLM_ROUTER_URL/v1/llm/generate/stream_text` (SSE: events `{"token":"...","done":false}` …
`{"token":"","done":true}`) AND parallel POST `/v1/llm/generate` (returns `{"brain": {...BrainOutput...}}`):
```json
{
  "user_turn": "<caller transcript>",
  "lang": "hi-en",
  "tenant_id": "...", "session_id": "...", "project_id": "...",
  "system_prompt_version": "v1",
  "dialog_history": [{"role":"user|assistant","content":"..."}],
  "persona_gender": "male|female",          // voice_gender.gender_for_speaker(ctx.voice_profile_id)
  "collected_slots": { } ,                   // accumulate across the call (budget/location/etc.)
  "system_prompt_suffix": "<CSO directive + optional [LOW_CONF_TURN]>",
  "campaign_context": { }                    // ctx.campaign_context when non-empty
}
```
BrainOutput fields (reply, lead_status, lead_score, next_action, should_send_whatsapp,
should_handover_to_human, should_create_site_visit, should_create_callback, risk_level, confidence,
summary, budget, location_pref, property_type, timeline_days, purpose). See models.py.

## Redis session (load at WS start) — key `call:session:{session_id}`
Fields → SessionContext: session_id, tenant_id, campaign_id, kb_version_id, voice_profile_id
(default env `SARVAM_TTS_SPEAKER`=`rahul`), lang(`hi-en`), system_prompt_version(`v1`), lead_id,
call_session_id, project_id, tts_premium(bool), campaign_context(dict). (Reuse models.SessionContext +
app._load_context logic.)

## Provider selection (per call)
- TTS engine: ElevenLabs if `ctx.tts_premium` OR `TTS_PREMIUM=1` OR campaign flag; else Sarvam `bulbul:v3`
  with speaker=`ctx.voice_profile_id`. Both must be available + switchable (founder requirement).
- STT: Sarvam `saaras:v3` (lang from ctx.lang). 
- Resampling: VobizFrameSerializer expects 8kHz PCM; ensure TTS output is resampled to 8k (Sarvam emits
  24k → Pipecat/transport resamples; ElevenLabs request 8k native). Confirm transport output rate = 8000.

## NATS events to reproduce (subjects + payloads) — see actions.py / events publisher
- every turn: `lead.status.updated` {session_id,tenant_id,lead_id,campaign_id,status,score}
- `call.handover.requested` / `call.site_visit.requested` / `call.callback.requested` /
  `call.whatsapp.requested` — gated by the matching BrainOutput flag (payloads per spec).
- every turn: `call.turn.recorded` = asdict(CallTurn). 
- once at end: `call.completed` {session_id,tenant_id,campaign_id,lead_id,project_id,outcome,status,
  summary,lead_status,lead_score,duration_s,turn_count,transcript[]}.
- JetStream streams CAPSY_CALL (`call.>`) / CAPSY_LEAD (`lead.>`); set `Nats-Msg-Id` dedup header.

## Env vars (reuse existing names)
LLM_ROUTER_URL(:8111), STT_ROUTER_URL(:8110, fallback only), TTS_ROUTER_URL(:8113, not used for TTS now),
GUARDRAIL_URL(:8112), REDIS_URL, NATS_URL, EVENT_PUBLISHER(nats|demo), SARVAM_TTS_SPEAKER(rahul),
TTS_PREMIUM, TTS_PACE(1.0), TTS_TEMPERATURE(0.6), GROQ_API_KEY/_2 + CSO_MODEL(llama-3.1-8b-instant)/
CSO_TIMEOUT_S(1.5)/CSO_ENABLED(true), LLM_HISTORY_MAX_MSGS(12), LOW_CONF_BARGE_IN_THRESHOLD(0.85).
NEW: PIPECAT_PORT(8140), VOICE_RUNTIME(old|pipecat) feature flag, plus DIRECT provider keys for Pipecat:
SARVAM_API_KEY (STT+TTS), ELEVENLABS_API_KEY + ELEVENLABS_VOICE_ID + ELEVENLABS_MODEL. (Values already
exist in ALL_CREDENTIALS.md / box env — wire by name, never hardcode.)

## Verification (local now; live on droplet later)
- Local: `python -m py_compile` all new files; `pytest` the mocked tests green. If pipecat installs on
  this machine, run a local WS harness feeding recorded µ-law → assert `{event:playAudio}` out.
- Droplet (needs server access): install requirements-pipecat.txt in the venv, run app on 127.0.0.1:8140,
  nginx routes `/pipecat/ws/` there, point ONE test campaign's Answer-URL at it, place a live call,
  assert: greeting plays, streaming TTS (no `1011`/REST fallback in logs), barge-in cuts audio,
  first-audio latency well under the old ~940ms median.

## Guardrails baked in
- Build only under `voice_agent/pipecat_runtime/`; do not modify the old worker files yet (parallel/flagged).
- Never hardcode secrets — `os.getenv` only. Bind app to 127.0.0.1. No new public port (nginx fronts it).
- Commit in small units on branch `pipecat-runtime`; tag `pipecat-pN-stable` per verified phase.
