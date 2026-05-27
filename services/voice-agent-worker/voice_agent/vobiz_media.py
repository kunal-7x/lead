"""vobiz_media.py — Vobiz µ-law WebSocket ↔ AgentLoop bridge.

This module is a pure codec/protocol adapter.  AgentLoop is untouched.
It translates between Vobiz's JSON media-stream protocol (Plivo/Twilio-style)
and the binary-PCM interface that AgentLoop already uses.

Inbound Vobiz frame shapes
--------------------------
"start" event (stream metadata):
    {
        "event": "start",
        "start": {
            "streamId": "<str>",    # Vobiz stream identifier
            "callId":   "<str>"     # maps to our internal_call_id
        }
    }

"media" event (audio from caller):
    {
        "event": "media",
        "media": {
            "payload": "<base64-encoded µ-law 8 kHz bytes>"
        }
    }

"stop" event (call ended by platform):
    {
        "event": "stop"
    }

Outbound Vobiz frame shape
--------------------------
NOTE: Vobiz bidirectional-stream outbound format is not publicly finalised.
The shape below mirrors the Plivo Bidirectional Media Stream spec, which uses
{"event":"playAudio","media":{...}}.  Twilio uses {"event":"media","media":{...}}.
We default to the Plivo shape; change _build_outbound_media_frame() if needed.

    {
        "event":  "playAudio",
        "media": {
            "contentType": "audio/x-mulaw",
            "sampleRate":  8000,
            "payload":     "<base64-encoded µ-law 8 kHz bytes>"
        }
    }

References:
  • Plivo Bidirectional Stream: https://www.plivo.com/docs/voice/api/call/stream
  • Vobiz-Pipecat repo: https://github.com/vobiz-ai/Vobiz-Pipecat
"""
from __future__ import annotations

import asyncio
import base64
import json
import logging
import sys
from typing import AsyncIterator

from voice_agent.agent import AgentLoop
from voice_agent.clients import HttpSTTClient, HttpLLMClient, HttpGuardrailClient, HttpTTSClient
from voice_agent.demo_runtime import DemoEventPublisher, DemoTurnStore
from voice_agent.models import SessionContext
from voice_agent.vad import SileroVAD
from voice_agent.vobiz_codec import ulaw_to_pcm16, pcm16_to_ulaw

logger = logging.getLogger(__name__)

# 20 ms of audio at 8 kHz µ-law = 160 samples = 160 bytes
_ULAW_CHUNK_BYTES = 160
# 20 ms of audio at 8 kHz PCM16 = 160 samples × 2 bytes = 320 bytes
_PCM_CHUNK_BYTES = 320


# ---------------------------------------------------------------------------
# Outbound frame builder
# ---------------------------------------------------------------------------

def _build_outbound_media_frame(ulaw_chunk: bytes) -> str:
    """Encode a µ-law audio chunk as a Vobiz outbound JSON frame string.

    NOTE: The exact "event" name and "media" sub-fields for Vobiz outbound audio
    are not definitively documented in a public spec at time of writing.
    This implementation mirrors the Plivo Bidirectional Media Stream shape:
        {"event":"playAudio","media":{"contentType":"audio/x-mulaw",
                                       "sampleRate":8000,"payload":"<b64>"}}
    Twilio uses {"event":"media","media":{"payload":"<b64>"}}.
    Change the constant _OUTBOUND_EVENT_NAME below if Vobiz uses a different key.
    """
    payload = base64.b64encode(ulaw_chunk).decode()
    frame = {
        "event": "playAudio",       # Plivo bidirectional stream convention
        "media": {
            "contentType": "audio/x-mulaw",
            "sampleRate": 8000,
            "payload": payload,
        },
    }
    return json.dumps(frame)


def _build_clear_audio_frame() -> str:
    """Vobiz clear-buffer frame, sent on barge-in (stop_playback).

    NOTE: Frame name assumed from Plivo convention ("clearAudio").
    Adjust if Vobiz uses a different event name.
    """
    return json.dumps({"event": "clearAudio"})


# ---------------------------------------------------------------------------
# Main bridge coroutine
# ---------------------------------------------------------------------------

async def run_vobiz_bridge(
    websocket,
    internal_call_id: str,
    *,
    _load_context_fn=None,
    _make_services_fn=None,
) -> None:
    """Handle a single Vobiz WebSocket call end-to-end.

    Parameters
    ----------
    websocket:
        A FastAPI WebSocket (or any object with .receive_text() / .send_text()).
    internal_call_id:
        Identifier for this call, used to load SessionContext from Redis.
    _load_context_fn:
        Injection point for tests — replaces the real _load_context helper.
    _make_services_fn:
        Injection point for tests — returns (stt, llm, guardrail, tts,
        publisher, store, vad) tuple.
    """
    # Import here to avoid circular at module level; app.py imports us
    from voice_agent.app import _load_context as _default_load_context

    load_ctx = _load_context_fn or _default_load_context

    # Stream metadata captured from the "start" frame, if Vobiz sends one.
    # We do NOT block on it — the bridge starts immediately so the caller is
    # never met with silence.
    stream_id: str | None = None
    ctx: SessionContext | None = None

    # PCM bytes accumulated between µ-law frames; drained into audio_source
    _pcm_queue: asyncio.Queue[bytes | None] = asyncio.Queue()

    def _log_raw(idx: int, raw) -> None:
        # print() to stderr guarantees visibility in the uvicorn .err log
        # regardless of logging config — critical for diagnosing the real
        # Vobiz frame shape on a live call.
        try:
            preview = raw[:300] if isinstance(raw, (str, bytes)) else repr(raw)[:300]
            print(f"[vobiz_bridge] RAW frame[{idx}] type={type(raw).__name__} {preview!r}",
                  file=sys.stderr, flush=True)
        except Exception:  # noqa: BLE001
            pass

    def _enqueue_ulaw(ulaw_data: bytes) -> None:
        pcm_data = ulaw_to_pcm16(ulaw_data)
        for offset in range(0, len(pcm_data), _PCM_CHUNK_BYTES):
            chunk = pcm_data[offset: offset + _PCM_CHUNK_BYTES]
            if len(chunk) < _PCM_CHUNK_BYTES:
                chunk = chunk + b"\x00" * (_PCM_CHUNK_BYTES - len(chunk))
            _pcm_queue.put_nowait(chunk)

    def _extract_payload(frame: dict) -> str:
        # Tolerant extraction across Plivo/Twilio/Vobiz variants.
        media = frame.get("media")
        if isinstance(media, dict):
            p = media.get("payload") or media.get("data")
            if p:
                return p
        return frame.get("payload", "") or ""

    async def _receive_loop() -> None:
        """Read WS frames (text JSON or raw binary µ-law) into the PCM queue."""
        nonlocal stream_id, ctx
        idx = 0
        try:
            while True:
                msg = await websocket.receive()
                mtype = msg.get("type")
                if mtype == "websocket.disconnect":
                    break

                # Binary frame → raw µ-law audio (some platforms stream bytes directly)
                if msg.get("bytes") is not None:
                    data = msg["bytes"]
                    if idx < 8:
                        _log_raw(idx, data); idx += 1
                    if data:
                        _enqueue_ulaw(data)
                    continue

                raw = msg.get("text")
                if raw is None:
                    continue
                if idx < 8:
                    _log_raw(idx, raw); idx += 1

                try:
                    frame = json.loads(raw)
                except (ValueError, TypeError):
                    continue
                if not isinstance(frame, dict):
                    continue
                event = (frame.get("event") or frame.get("type") or "").lower()

                if event == "start":
                    meta = frame.get("start", {}) if isinstance(frame.get("start"), dict) else {}
                    stream_id = meta.get("streamId") or meta.get("streamSid") or frame.get("streamId") or internal_call_id
                    print(f"[vobiz_bridge] start stream_id={stream_id}", file=sys.stderr, flush=True)
                elif event in ("media", "audio"):
                    payload_b64 = _extract_payload(frame)
                    if payload_b64:
                        try:
                            _enqueue_ulaw(base64.b64decode(payload_b64))
                        except Exception:  # noqa: BLE001
                            pass
                elif event in ("stop", "closed"):
                    print(f"[vobiz_bridge] stop stream_id={stream_id}", file=sys.stderr, flush=True)
                    break

        except Exception as exc:  # noqa: BLE001
            print(f"[vobiz_bridge] receive_loop ended: {exc!r}", file=sys.stderr, flush=True)
        finally:
            await _pcm_queue.put(None)  # sentinel: signal audio_source to stop

    async def audio_source() -> AsyncIterator[bytes]:
        """Async generator feeding PCM16 chunks to AgentLoop."""
        while True:
            chunk = await _pcm_queue.get()
            if chunk is None:
                return
            yield chunk

    async def send_audio(pcm_bytes: bytes) -> None:
        """Convert PCM16 → µ-law, chunk into 20ms frames, send at 20ms cadence.

        Pacing is critical: bursting all frames instantly causes the Vobiz jitter
        buffer to overflow and then starve — heard as choppy / breaking audio.
        Each 160-byte µ-law frame = 20ms @ 8kHz, so we wait ~20ms between frames.
        We subtract actual send time from the sleep to stay on pace even under load.
        """
        ulaw_data = pcm16_to_ulaw(pcm_bytes)
        n_frames = (len(ulaw_data) + _ULAW_CHUNK_BYTES - 1) // _ULAW_CHUNK_BYTES
        if n_frames == 0:
            return

        # Real-time pacing: track when we should have sent each frame
        _FRAME_DURATION = 0.020  # 20ms per frame
        t_start = asyncio.get_event_loop().time()

        for i, offset in enumerate(range(0, len(ulaw_data), _ULAW_CHUNK_BYTES)):
            chunk = ulaw_data[offset: offset + _ULAW_CHUNK_BYTES]
            # Pad last partial frame to full 160 bytes
            if len(chunk) < _ULAW_CHUNK_BYTES:
                chunk = chunk + b"\xff" * (_ULAW_CHUNK_BYTES - len(chunk))
            await websocket.send_text(_build_outbound_media_frame(chunk))

            # Pace: sleep until the next frame's scheduled send time
            # This keeps the stream at real-time rate; avoids burst-then-underrun.
            # We do NOT pace the very last frame (no need to wait after EOF).
            if i < n_frames - 1:
                next_frame_time = t_start + (i + 1) * _FRAME_DURATION
                now = asyncio.get_event_loop().time()
                sleep_s = next_frame_time - now
                if sleep_s > 0.001:  # only sleep if > 1ms remains
                    await asyncio.sleep(sleep_s)

    async def send_json(msg: dict) -> None:
        """Translate AgentLoop control messages to Vobiz equivalents."""
        msg_type = msg.get("type", "")
        if msg_type == "stop_playback":
            # Barge-in: tell Vobiz to clear its audio buffer
            await websocket.send_text(_build_clear_audio_frame())
        elif msg_type == "call_complete":
            # No standard Vobiz call-complete frame; log only
            logger.info("vobiz_bridge call_complete stream_id=%s outcome=%s",
                        stream_id, msg.get("outcome", "unknown"))
        else:
            # Unknown control message — log and drop
            logger.debug("vobiz_bridge send_json ignored: %s", msg)

    # Load context immediately from the internal call id (fallback to default
    # inside _load_context if Redis has nothing). We do NOT wait for a "start"
    # frame — Vobiz/Plivo may not send one in the shape we expect, and blocking
    # leaves the caller in silence. The receive loop captures stream_id later.
    ctx = await load_ctx(internal_call_id)

    # Start the receive loop in background; it populates _pcm_queue.
    receive_task = asyncio.create_task(_receive_loop())

    # Instantiate services
    if _make_services_fn is not None:
        stt, llm, guardrail, tts, publisher, store, vad = _make_services_fn()
    else:
        stt = HttpSTTClient()
        llm = HttpLLMClient()
        guardrail = HttpGuardrailClient()
        tts = HttpTTSClient()
        publisher = DemoEventPublisher()
        store = DemoTurnStore()
        vad = SileroVAD()

    # Synthesize an opening greeting in Hindi so the caller hears the agent
    # immediately on answer. Uses a warm real-estate telecaller opener in Hindi.
    # Falls back gracefully on TTS failure (no greeting rather than crash).
    greeting_audio: bytes | None = None
    # Use session greeting if set (e.g. custom per-campaign), else natural Hindi opener
    greeting_text = getattr(ctx, "greeting", None) or (
        "नमस्ते! मैं आपको प्रॉपर्टी के बारे में जानकारी देने के लिए कॉल कर रही हूँ। "
        "क्या आप अभी बात कर सकते हैं?"
    )
    try:
        greeting_result = await tts.synthesize(
            greeting_text, ctx.lang, ctx.voice_profile_id,
            ctx.tenant_id, ctx.session_id, ctx.tts_premium,
        )
        greeting_audio = greeting_result.audio
        print(f"[vobiz_bridge] greeting synthesized: {len(greeting_audio)} bytes, text={greeting_text!r}",
              file=sys.stderr, flush=True)
    except Exception as exc:  # noqa: BLE001
        logger.warning("vobiz_bridge greeting TTS failed, no greeting: %r", exc)

    loop = AgentLoop(ctx, stt, llm, guardrail, tts, publisher, store, vad,
                     greeting_audio=greeting_audio)

    try:
        await loop.run(audio_source(), send_audio, send_json)
    finally:
        receive_task.cancel()
        try:
            await receive_task
        except (asyncio.CancelledError, Exception):
            pass
