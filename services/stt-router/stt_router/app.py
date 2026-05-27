from __future__ import annotations

import asyncio
import json
import logging
import os
import time

import redis.asyncio as aioredis
from fastapi import FastAPI, WebSocket, WebSocketDisconnect, UploadFile, File
from fastapi.responses import JSONResponse

from stt_router.engines.sarvam import SarvamEngine, SarvamStreamingEngine
from stt_router.engines.indicconformer import IndicConformerEngine
from stt_router.engines.faster_whisper import FasterWhisperEngine
from stt_router.engines.groq_whisper import GroqWhisperEngine
from stt_router.models import STTRequest, STTResult, HealthResponse
from stt_router.router import STTRouter
from stt_router.switcher import EngineSwitcher

# Feature flag: "disabled" (default, batch-only) | "sarvam" (enables SarvamStreamingEngine)
# Keeping "sarvam_streaming" as a legacy alias so existing deployments that set that value
# continue to work without a coordinated env-var change.
_STT_STREAMING_ENGINE = os.getenv("STT_STREAMING_ENGINE", "disabled")

logger = logging.getLogger("stt_router.stream")

app = FastAPI(title="stt-router", version="0.1.0")

_router: STTRouter | None = None
_streaming_engine: SarvamStreamingEngine | None = None


def _build_router() -> STTRouter:
    redis_url = os.getenv("REDIS_URL", "redis://localhost:6379")
    rdb = aioredis.from_url(redis_url, decode_responses=False)
    switcher = EngineSwitcher(rdb)
    engines = {
        "sarvam": SarvamEngine(),
        "indicconformer": IndicConformerEngine(),
        "faster_whisper": FasterWhisperEngine(),
        "groq_whisper": GroqWhisperEngine(),
    }
    return STTRouter(engines, switcher)


@app.on_event("startup")
async def startup() -> None:
    global _router, _streaming_engine
    _router = _build_router()
    if _STT_STREAMING_ENGINE in ("sarvam", "sarvam_streaming"):
        # "sarvam" is the canonical flag value; "sarvam_streaming" kept as legacy alias
        _streaming_engine = SarvamStreamingEngine()
    else:
        # Explicitly reset to None so tests using disabled flag don't inherit
        # a streaming engine set by a previous TestClient's startup lifecycle.
        _streaming_engine = None


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok"}


@app.get("/v1/health/engines", response_model=HealthResponse)
async def engine_health() -> HealthResponse:
    assert _router is not None
    engines = await _router.engine_health()
    return HealthResponse(engines=engines)


@app.post("/v1/stt/batch")
async def stt_batch(
    file: UploadFile = File(...),
    lang: str = "hi-en",
    session_id: str = "batch",
    tenant_id: str = "default",
) -> JSONResponse:
    assert _router is not None
    audio = await file.read()
    result = await _router.transcribe(audio, lang, session_id, tenant_id)
    return JSONResponse(result.model_dump())


@app.websocket("/v1/stt/stream")
async def stt_stream(
    websocket: WebSocket,
    lang: str | None = None,
    session_id: str | None = None,
    tenant_id: str = "default",
) -> None:
    """Bidirectional WebSocket STT stream — supports two protocols:

    NEW (query-param) protocol — used when STT_STREAMING_ENGINE=sarvam:
      CONNECT: ws://stt-router:8110/v1/stt/stream?lang=hi-en&session_id=X
      SEND:    raw binary PCM16 8kHz frames (320 bytes = 20ms per frame)
               Sentinel: empty binary frame (0 bytes) = end-of-utterance
      RECEIVE: {"type": "interim", "text": "...", "ts": float}
               {"type": "final",   "text": "...", "confidence": 0.9,
                "latency_ms": 302, "engine_used": "sarvam_streaming"}
               {"type": "error",   "message": "..."}
      If STT_STREAMING_ENGINE != "sarvam"/"sarvam_streaming": sends
               {"type": "error", "message": "streaming_disabled"} then closes.

    LEGACY (JSON-header) protocol — unchanged when lang not in query params:
      First message: JSON {lang, session_id, tenant_id}
      Subsequent messages:
        - binary L16 PCM frames — transcribed per-frame immediately
        - TEXT {"type":"flush"} — transcribe accumulated buffer, reset
      Server sends: JSON STTResult for each transcribed chunk / flush
    """
    await websocket.accept()

    # ── NEW STREAMING PROTOCOL ──────────────────────────────────────────────
    # Activated when `lang` is supplied as a query param (new worker contract).
    if lang is not None:
        if _streaming_engine is None:
            # Streaming flag is off — inform caller immediately.
            # Returning from the handler causes Starlette to close the WS cleanly.
            await websocket.send_text(json.dumps({
                "type": "error",
                "message": "streaming_disabled",
                "hint": "Set STT_STREAMING_ENGINE=sarvam to enable realtime STT",
            }))
            return

        sid = session_id or "stream"
        # TRUE during-speech streaming: open the Sarvam WS now (via stream_live)
        # and forward each PCM16 frame to Sarvam AS IT ARRIVES. On the 0-byte
        # sentinel we send {"type":"flush"} and return the final — which arrives
        # in ~150-300ms because Sarvam already processed the audio during speech.
        #
        # We keep a per-utterance copy of all forwarded frames so that if the
        # live stream errors we can still fall back to batch on the same audio.
        try:
            while True:
                # ── Wait for the FIRST frame of the next utterance ──────────────
                # An immediate 0-byte sentinel (no audio) short-circuits to an
                # empty final WITHOUT opening a Sarvam WS — matches the legacy
                # empty-utterance contract and avoids a wasted round-trip.
                first_frame: bytes | None = None
                disconnected = False
                while True:
                    m0 = await websocket.receive()
                    if m0["type"] == "websocket.disconnect":
                        disconnected = True
                        break
                    fb = m0.get("bytes")
                    if fb is None:
                        continue
                    first_frame = fb
                    break

                if disconnected:
                    break

                if first_frame is not None and len(first_frame) == 0:
                    # Empty utterance — no audio buffered.
                    await websocket.send_text(json.dumps({
                        "type": "final", "text": "", "confidence": 1.0,
                        "latency_ms": 0, "engine_used": "none",
                    }))
                    continue

                frame_queue: asyncio.Queue = asyncio.Queue()
                utterance_bytes = bytearray()  # full copy for batch fallback
                got_any_frame = True
                # Seed with the first frame already read.
                utterance_bytes.extend(first_frame)
                frame_queue.put_nowait(first_frame)

                # Collect remaining frames for ONE utterance: feed the queue live,
                # copy for fallback. Returns ("sentinel"|"disconnect").
                async def _collect_utterance() -> str:
                    while True:
                        m = await websocket.receive()
                        if m["type"] == "websocket.disconnect":
                            await frame_queue.put(None)
                            return "disconnect"
                        b = m.get("bytes")
                        if b is None:
                            continue
                        if len(b) == 0:
                            await frame_queue.put(None)  # end-of-utterance sentinel
                            return "sentinel"
                        utterance_bytes.extend(b)
                        await frame_queue.put(b)

                collector = asyncio.create_task(_collect_utterance())

                # Drain the live stream concurrently with frame collection.
                final_sent = False
                stream_error: str | None = None
                t_stream_start = time.time()
                try:
                    async for event in _streaming_engine.stream_live(
                        frame_queue,
                        lang=lang,
                        session_id=sid,
                        audio_format="pcm16",
                    ):
                        if event["type"] == "error":
                            stream_error = event.get("message", "stream_error")
                            break
                        await websocket.send_text(json.dumps(event))
                        if event["type"] == "final":
                            final_sent = True
                            logger.info(
                                "streaming success session=%s lang=%s latency_ms=%s "
                                "wall_ms=%d text_len=%d",
                                sid, lang, event.get("latency_ms"),
                                int((time.time() - t_stream_start) * 1000),
                                len(event.get("text", "")),
                            )
                            break
                except Exception as exc:  # noqa: BLE001
                    stream_error = repr(exc)

                collect_outcome = await collector

                # ── Batch fallback — only when live streaming genuinely failed ──
                if not final_sent and got_any_frame:
                    logger.warning(
                        "fell back to batch (reason=%s) session=%s lang=%s bytes=%d",
                        stream_error or "no_final", sid, lang, len(utterance_bytes),
                    )
                    assert _router is not None
                    try:
                        result = await _router.transcribe(
                            bytes(utterance_bytes), lang, sid, tenant_id
                        )
                        await websocket.send_text(json.dumps({
                            "type": "final",
                            "text": result.text,
                            "confidence": result.confidence,
                            "latency_ms": result.latency_ms,
                            "engine_used": result.engine_used,
                        }))
                    except Exception as exc:  # noqa: BLE001
                        await websocket.send_text(json.dumps({
                            "type": "error", "message": f"batch_fallback_failed: {exc!r}",
                        }))

                if collect_outcome == "disconnect":
                    break

        except WebSocketDisconnect:
            pass
        except Exception as exc:
            try:
                await websocket.send_text(json.dumps({"type": "error", "message": str(exc)}))
                await websocket.close()
            except Exception:
                pass
        return

    # ── LEGACY (JSON-HEADER) PROTOCOL — 100% UNCHANGED ─────────────────────
    assert _router is not None
    req: STTRequest | None = None
    legacy_buffer: bytearray = bytearray()

    try:
        # First message must be JSON header
        header_raw = await websocket.receive_text()
        req = STTRequest.model_validate_json(header_raw)

        # Stream binary audio frames or control messages
        while True:
            msg = await websocket.receive()

            if msg["type"] == "websocket.receive":
                if "bytes" in msg and msg["bytes"] is not None:
                    # Binary frame: accumulate audio
                    legacy_buffer.extend(msg["bytes"])
                    # Auto-transcribe each frame (existing behaviour)
                    result = await _router.transcribe(
                        bytes(msg["bytes"]), req.lang, req.session_id, req.tenant_id
                    )
                    await websocket.send_text(result.model_dump_json())

                elif "text" in msg and msg["text"] is not None:
                    try:
                        ctrl = json.loads(msg["text"])
                    except Exception:
                        ctrl = {}

                    if ctrl.get("type") == "flush":
                        if legacy_buffer:
                            result = await _router.transcribe(
                                bytes(legacy_buffer), req.lang, req.session_id, req.tenant_id
                            )
                        else:
                            _ts = time.time()
                            result = STTResult(
                                text="",
                                confidence=1.0,
                                language=req.lang,
                                engine_used="none",
                                is_final=True,
                                latency_ms=0,
                                turn_start_ts=_ts,
                                turn_end_ts=_ts,
                            )
                        await websocket.send_text(result.model_dump_json())
                        legacy_buffer = bytearray()

            elif msg["type"] == "websocket.disconnect":
                break

    except WebSocketDisconnect:
        pass
    except Exception as exc:
        try:
            await websocket.send_text(json.dumps({"error": str(exc)}))
            await websocket.close()
        except Exception:
            pass


@app.websocket("/v1/stt/realtime")
async def stt_realtime(
    websocket: WebSocket,
    lang: str = "hi-en",
    session_id: str = "rt",
    tenant_id: str = "default",
    audio_format: str = "ulaw",  # "ulaw" (G.711 µ-law 8kHz) or "pcm16" (PCM16 8kHz)
) -> None:
    """True streaming STT WebSocket — transcription happens WHILE caller talks.

    Worker-facing contract:
      CONNECT: ws://stt-router:8110/v1/stt/realtime?lang=hi-en&session_id=X&audio_format=ulaw
      SEND:    raw binary frames — G.711 µ-law 8kHz bytes (what Vobiz gives the worker)
               OR PCM16 8kHz bytes when audio_format=pcm16
               Sentinel: empty binary frame (0 bytes) = end of utterance / flush
      RECEIVE: {"type": "interim", "text": "...", "ts": float}       — VAD speech start
               {"type": "final",   "text": "...", "confidence": 0.9,
                "latency_ms": 302, "engine_used": "sarvam_streaming"} — on endpointing
               {"type": "error",   "message": "..."}                 — on failure

    Fallback: if STT_STREAMING_ENGINE=disabled (or streaming engine fails),
              falls back to batch transcription via the main STTRouter.

    Audio format the worker sends: raw µ-law 8kHz bytes (default audio_format=ulaw).
    Conversion (µ-law → PCM16 → WAV → base64) is done inside the engine.
    """
    await websocket.accept()
    audio_buffer: bytearray = bytearray()

    try:
        while True:
            msg = await websocket.receive()
            if msg["type"] == "websocket.disconnect":
                break

            if msg.get("bytes") is not None:
                frame = msg["bytes"]

                # Empty frame = end-of-utterance sentinel → transcribe
                if len(frame) == 0:
                    if not audio_buffer:
                        await websocket.send_text(json.dumps({
                            "type": "final", "text": "", "confidence": 1.0,
                            "latency_ms": 0, "engine_used": "none",
                        }))
                        audio_buffer = bytearray()
                        continue

                    audio_bytes = bytes(audio_buffer)
                    audio_buffer = bytearray()

                    # Try streaming engine first
                    if _streaming_engine is not None:
                        try:
                            async for event in _streaming_engine.stream_utterance(
                                audio_bytes,
                                lang=lang,
                                session_id=session_id,
                                audio_format=audio_format,
                            ):
                                await websocket.send_text(json.dumps(event))
                                if event["type"] in ("final", "error"):
                                    break
                            continue
                        except Exception as exc:
                            # Streaming failed — fall through to batch
                            pass

                    # Batch fallback
                    assert _router is not None
                    # Transcode µ-law → PCM16 for batch engine if needed
                    if audio_format == "ulaw":
                        from stt_router.engines.sarvam import ulaw_to_pcm16
                        pcm = ulaw_to_pcm16(audio_bytes)
                    else:
                        pcm = audio_bytes
                    result = await _router.transcribe(pcm, lang, session_id, tenant_id)
                    await websocket.send_text(json.dumps({
                        "type": "final",
                        "text": result.text,
                        "confidence": result.confidence,
                        "latency_ms": result.latency_ms,
                        "engine_used": result.engine_used,
                    }))

                else:
                    # Accumulate audio frames
                    audio_buffer.extend(frame)

            elif msg.get("text") is not None:
                # Text control: {"type": "flush"} forces immediate transcription
                try:
                    ctrl = json.loads(msg["text"])
                except Exception:
                    ctrl = {}

                if ctrl.get("type") == "flush" and audio_buffer:
                    audio_bytes = bytes(audio_buffer)
                    audio_buffer = bytearray()

                    if _streaming_engine is not None:
                        try:
                            async for event in _streaming_engine.stream_utterance(
                                audio_bytes,
                                lang=lang,
                                session_id=session_id,
                                audio_format=audio_format,
                            ):
                                await websocket.send_text(json.dumps(event))
                                if event["type"] in ("final", "error"):
                                    break
                            continue
                        except Exception:
                            pass

                    assert _router is not None
                    if audio_format == "ulaw":
                        from stt_router.engines.sarvam import ulaw_to_pcm16
                        pcm = ulaw_to_pcm16(audio_bytes)
                    else:
                        pcm = audio_bytes
                    result = await _router.transcribe(pcm, lang, session_id, tenant_id)
                    await websocket.send_text(json.dumps({
                        "type": "final",
                        "text": result.text,
                        "confidence": result.confidence,
                        "latency_ms": result.latency_ms,
                        "engine_used": result.engine_used,
                    }))

    except WebSocketDisconnect:
        pass
    except Exception as exc:
        try:
            await websocket.send_text(json.dumps({"type": "error", "message": str(exc)}))
            await websocket.close()
        except Exception:
            pass
