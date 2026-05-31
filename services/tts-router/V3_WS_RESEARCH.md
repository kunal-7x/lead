# Sarvam bulbul:v3 over WebSocket — Research Findings (2026-06-01)

Goal: can the C13 Hindi telecaller use bulbul:v3 (priya) over the **streaming
WebSocket** path (not just REST)? If so, what is the exact contract, and why did
our earlier attempt silently get v2 (anushka) on the WS?

## TL;DR VERDICT
**YES — bulbul:v3 IS available over the WebSocket TTS API as of 2026.** It is the
top performer at 8 kHz telephony. Our earlier attempt got v2 because we passed the
model **only inside the config JSON frame**. The WS selects the model from a
**URL query parameter** `?model=bulbul:v3`; with no query param it **defaults to
`bulbul:v2`**. A v2 session then rejects v3 speakers like `priya` ("not compatible
with model bulbul:v2") — which is exactly the error we saw. The config-frame
`model` field does NOT switch the engine.

## ROOT CAUSE (why we got v2/anushka on the WS before)
- Our `_WS_URL = "wss://api.sarvam.ai/text-to-speech/ws"` had **no query string**.
- We put `"model": "bulbul:v3"` inside the `config` frame `data`. The WS ignores
  that for engine selection → session ran as the **default `bulbul:v2`**.
- v3 speaker `priya` on a v2 session → error → we "fixed" it by mapping the WS
  path to a v2 speaker (`anushka`). That made the WS work but locked it to v2.
- Net effect: greeting/REST = v3 priya (REST honours the `model` body field),
  streaming WS = v2 anushka → audible quality drop mid-call.

## EXACT WS CONTRACT (current)
- **Endpoint:** `wss://api.sarvam.ai/text-to-speech/ws`
- **Model selection (CRITICAL):** query param on the URL.
  - `wss://api.sarvam.ai/text-to-speech/ws?model=bulbul:v3`
  - Default if omitted = `bulbul:v2`.
  - Optional second query param: `send_completion_event=true|false` (default true).
- **Auth header:** `api-subscription-key: <SARVAM_API_KEY>` (case-insensitive;
  REST uses `API-Subscription-Key` — same key).
- **Config frame (first message):**
  ```json
  {"type":"config","data":{
    "target_language_code":"hi-IN",
    "speaker":"priya",
    "speech_sample_rate":8000,
    "output_audio_codec":"mulaw",
    "temperature":0.6,
    "pace":1.0,
    "min_buffer_size":50,
    "max_chunk_length":150
  }}
  ```
  - v3 rules: **no `pitch`, no `loudness`** (v2-only; v3 rejects them).
    `enable_preprocessing` is always-on for v3 (sending it is harmless).
    `temperature` 0.01–1.0 is v3-only. `pace` v3 range 0.5–2.0.
  - mulaw + 8000 is supported → µ-law 8k DIRECT on the wire (telephony grade).
- **Text frame:** `{"type":"text","data":{"text":"..."}}`
- **Flush frame:** `{"type":"flush"}`
- **Keepalive:** application-level `{"type":"ping"}` (WS-level ping ignored).
- **Completion:** when `send_completion_event` is on, server emits an event with
  `event_type:"final"` to mark end-of-utterance. (Our older code relied on an
  idle-gap heuristic because v2 never sent a completion event; with v3+query-param
  we should honour the `final` event and KEEP the idle-gap as fallback.)

## SOURCES
- Sarvam WS TTS reference (model is a query param; default bulbul:v2;
  send_completion_event query param; v3 speaker list incl. priya):
  https://docs.sarvam.ai/api-reference-docs/text-to-speech/stream
- Sarvam WS streaming guide (mulaw codec supported; `connect(model="bulbul:v3")`;
  `event_type:"final"` completion):
  https://docs.sarvam.ai/api-reference-docs/api-guides-tutorials/text-to-speech/streaming-api/web-socket
- Sarvam Bulbul model reference (v3: no pitch/loudness, pace 0.5–2.0, temperature,
  default 24k; v3 speaker list):
  https://docs.sarvam.ai/api-reference-docs/getting-started/models/bulbul
- Bulbul V3 blog (released 2026-02-05; #1 at 8 kHz telephony; 35+ voices):
  https://www.sarvam.ai/blogs/bulbul-v3
- Pipecat Sarvam TTS plugin — CONFIRMS model is a query param, not just config:
  `self._websocket_url = f"{url}?model={resolved_model}"`,
  `resolved_model ∈ {bulbul:v2, bulbul:v3-beta, bulbul:v3}`, default `bulbul:v2`:
  https://reference-server.pipecat.ai/en/latest/_modules/pipecat/services/sarvam/tts.html
- LiveKit Sarvam plugin (corroborates WS streaming TTS contract):
  https://docs.livekit.io/reference/python/livekit/plugins/sarvam/index.html

## IMPLEMENTATION DECISION
Switch the streaming WS path to **bulbul:v3 / priya** by:
1. Appending `?model=<SARVAM_TTS_MODEL>&send_completion_event=true` to the WS URL.
2. Using the v3 speaker on the WS path (drop the v2-speaker remap when model is v3).
3. Dropping pitch/loudness on v3 (already done for REST).
4. Honouring the `event_type:"final"` completion event AND keeping the idle-gap
   fallback (belt-and-suspenders; idle gap can be shortened since `final` arrives).
5. Probe the live WS to CONFIRM it serves v3/priya (not silently v2) before trusting.

This makes the WHOLE call v3/priya — greeting (REST v3) and conversation (WS v3)
both the same good voice, no mid-call drop, and keeps µ-law 8k + synth-ahead.
