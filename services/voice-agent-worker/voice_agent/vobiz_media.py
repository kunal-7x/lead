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

    # We need the "start" frame first to get stream metadata before anything else.
    stream_id: str | None = None
    ctx: SessionContext | None = None

    # PCM bytes accumulated between µ-law frames; drained into audio_source
    _pcm_queue: asyncio.Queue[bytes | None] = asyncio.Queue()

    async def _receive_loop() -> None:
        """Read WebSocket frames, decode µ-law, put PCM into queue."""
        nonlocal stream_id, ctx
        try:
            while True:
                raw = await websocket.receive_text()
                frame = json.loads(raw)
                event = frame.get("event", "")

                if event == "start":
                    meta = frame.get("start", {})
                    stream_id = meta.get("streamId", internal_call_id)
                    call_id = meta.get("callId", internal_call_id)
                    ctx = await load_ctx(call_id)
                    logger.info("vobiz_bridge start stream_id=%s call_id=%s", stream_id, call_id)

                elif event == "media":
                    if ctx is None:
                        # "start" hasn't arrived yet — skip
                        continue
                    payload_b64 = frame.get("media", {}).get("payload", "")
                    if not payload_b64:
                        continue
                    ulaw_data = base64.b64decode(payload_b64)
                    pcm_data = ulaw_to_pcm16(ulaw_data)
                    # Repacketise into 320-byte (20ms) PCM chunks
                    for offset in range(0, len(pcm_data), _PCM_CHUNK_BYTES):
                        chunk = pcm_data[offset: offset + _PCM_CHUNK_BYTES]
                        if len(chunk) < _PCM_CHUNK_BYTES:
                            # Pad final short chunk to full 20ms
                            chunk = chunk + b"\x00" * (_PCM_CHUNK_BYTES - len(chunk))
                        await _pcm_queue.put(chunk)

                elif event == "stop":
                    logger.info("vobiz_bridge stop stream_id=%s", stream_id)
                    break

        except Exception as exc:  # noqa: BLE001
            logger.debug("vobiz_bridge receive_loop ended: %s", exc)
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
        """Convert PCM16 → µ-law, chunk into 20ms frames, send outbound."""
        ulaw_data = pcm16_to_ulaw(pcm_bytes)
        for offset in range(0, len(ulaw_data), _ULAW_CHUNK_BYTES):
            chunk = ulaw_data[offset: offset + _ULAW_CHUNK_BYTES]
            await websocket.send_text(_build_outbound_media_frame(chunk))

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

    # Wait for "start" frame before spinning up services.
    # Start the receive loop in background; it will populate _pcm_queue.
    receive_task = asyncio.create_task(_receive_loop())

    # Block until ctx is populated (the "start" frame arrived)
    while ctx is None:
        if receive_task.done():
            # WS closed before "start" — nothing to do
            return
        await asyncio.sleep(0.01)

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

    loop = AgentLoop(ctx, stt, llm, guardrail, tts, publisher, store, vad)

    try:
        await loop.run(audio_source(), send_audio, send_json)
    finally:
        receive_task.cancel()
        try:
            await receive_task
        except (asyncio.CancelledError, Exception):
            pass
