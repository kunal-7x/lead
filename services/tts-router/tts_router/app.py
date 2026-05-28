from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from typing import AsyncIterator

import redis.asyncio as aioredis
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from fastapi.responses import JSONResponse, Response

from tts_router.cache import RedisAudioCache
from tts_router.engines.sarvam import SarvamBulbulEngine, SarvamStreamingSession
from tts_router.engines.kokoro import KokoroEngine, IndicParlerEngine, IndicF5Engine, ElevenLabsEngine
from tts_router.models import HealthResponse, TTSRequest, WarmRequest
from tts_router.router import TTSRouter
from tts_router.switcher import EngineSwitcher

log = logging.getLogger(__name__)

# Primary TTS engine selection
# TTS_PRIMARY=sarvam (default) — Sarvam Bulbul is primary; ElevenLabs is optional fallback
# TTS_ELEVENLABS_ENABLED=false (default) — ElevenLabs disabled (returns 402 on free tier)
# SARVAM_TTS_MODEL — override Sarvam model slug (default bulbul:v2)
_TTS_ELEVENLABS_ENABLED = os.getenv("TTS_ELEVENLABS_ENABLED", "false").lower() == "true"
# Sarvam bulbul:v3 streaming-TTS WS path. DEFAULT OFF — flipping it on changes the
# real-time audio path; a regression means SILENT calls. When false, the worker and
# the batch endpoints use the safe REST synthesize() path UNCHANGED.
_TTS_STREAMING_WS = os.getenv("TTS_STREAMING_WS", "false").lower() == "true"

app = FastAPI(title="tts-router", version="0.1.0")
_router: TTSRouter | None = None
_elevenlabs_engine: ElevenLabsEngine | None = None


def _build_router() -> TTSRouter:
    rdb = aioredis.from_url(os.getenv("REDIS_URL", "redis://localhost:6379"))
    switcher = EngineSwitcher(rdb)
    cache = RedisAudioCache(rdb)
    # Sarvam Bulbul is always primary; ElevenLabs only included when explicitly enabled
    engines: dict = {
        "sarvam_bulbul": SarvamBulbulEngine(),
        "kokoro": KokoroEngine(),
        "indic_parler": IndicParlerEngine(),
        "indicf5": IndicF5Engine(),
    }
    if _TTS_ELEVENLABS_ENABLED:
        engines["elevenlabs"] = ElevenLabsEngine()
    return TTSRouter(engines, switcher, cache)


@app.on_event("startup")
async def startup() -> None:
    global _router, _elevenlabs_engine
    _router = _build_router()
    # Only initialise ElevenLabs engine instance if the feature-flag is on
    if _TTS_ELEVENLABS_ENABLED:
        _elevenlabs_engine = ElevenLabsEngine()


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok"}


@app.get("/v1/health/engines", response_model=HealthResponse)
async def engine_health() -> HealthResponse:
    assert _router is not None
    engines = await _router.engine_health()
    return HealthResponse(engines=engines)


@app.get("/v1/tts/voices")
async def voices() -> JSONResponse:
    assert _router is not None
    return JSONResponse([v.model_dump() for v in _router.all_voices()])


@app.post("/v1/tts/synthesize")
async def synthesize(req: TTSRequest) -> Response:
    assert _router is not None
    result = await _router.synthesize(req)
    return Response(
        content=result.audio,
        media_type="audio/pcm",
        headers={
            "X-Tier-Used": result.tier_used,
            "X-Latency-Ms": str(result.latency_ms),
            "X-Cache-Hit": str(result.cache_hit).lower(),
            "X-Sample-Rate": "8000",
        },
    )


@app.post("/v1/tts/cache/warm")
async def warm(req: WarmRequest) -> JSONResponse:
    assert _router is not None
    count = await _router.warm(req.phrases, req.lang, req.voice_id, req.tenant_id)
    return JSONResponse({"warmed": count, "total": len(req.phrases)})


@app.websocket("/v1/tts/ws")
async def tts_stream_ws(websocket: WebSocket) -> None:
    """ElevenLabs input-streaming WebSocket endpoint.

    CONTRACT:
      Client → Server (text JSON frames):
        {"text": "<token>", "voice_id": "<id>", "lang": "hi-en"}
        Send tokens incrementally as LLM produces them.
        Send {"text": "", "flush": true} to signal end-of-stream.

      Server → Client (binary frames):
        Raw µ-law 8kHz audio bytes (ulaw_8000), ready to forward to Vobiz.

      Server → Client (text JSON on completion):
        {"type": "done", "latency_ms": <int>}

      Server → Client (text JSON on error):
        {"type": "error", "message": "<str>"}

    Fallback: if ElevenLabs key missing or WS fails, falls back to
    batch Sarvam synthesis of accumulated text, returning same µ-law format.

    Feature-flag: TTS_STREAMING_WS=false → always uses batch fallback.
    """
    await websocket.accept()
    engine = _elevenlabs_engine
    if engine is None:
        engine = ElevenLabsEngine()

    # Collect incoming text into a queue for the async iterator
    text_queue: asyncio.Queue[str | None] = asyncio.Queue()

    async def _receive_loop() -> None:
        """Read from client WS, push tokens into queue."""
        try:
            while True:
                raw = await websocket.receive_text()
                msg = json.loads(raw)
                text = msg.get("text", "")
                flush = msg.get("flush", False)
                if text:
                    await text_queue.put(text)
                if flush or text == "":
                    await text_queue.put(None)  # sentinel = end of stream
                    break
        except WebSocketDisconnect:
            await text_queue.put(None)
        except Exception as exc:
            log.warning("tts_stream_ws receive error: %s", exc)
            await text_queue.put(None)

    async def _text_iter() -> AsyncIterator[str]:
        while True:
            token = await text_queue.get()
            if token is None:
                return
            yield token

    elevenlabs_api_key = os.getenv("ELEVENLABS_API_KEY", "")
    streaming_enabled = (
        _TTS_ELEVENLABS_ENABLED
        and elevenlabs_api_key
        and os.getenv("TTS_STREAMING_WS", "true").lower() != "false"
    )

    t0 = time.time()
    first_chunk_sent = False

    receive_task = asyncio.ensure_future(_receive_loop())

    try:
        if streaming_enabled and engine is not None:
            # Optional path: ElevenLabs input-streaming WS (only when TTS_ELEVENLABS_ENABLED=true)
            voice_id = os.getenv("ELEVENLABS_VOICE_ID", "")
            try:
                async for chunk in engine.synthesize_stream(
                    _text_iter(), voice_id=voice_id
                ):
                    await websocket.send_bytes(chunk)
                    if not first_chunk_sent:
                        ttfb_ms = int((time.time() - t0) * 1000)
                        log.info("ElevenLabs stream TTFB: %dms", ttfb_ms)
                        first_chunk_sent = True
                latency_ms = int((time.time() - t0) * 1000)
                await websocket.send_text(
                    json.dumps({"type": "done", "latency_ms": latency_ms, "engine": "elevenlabs"})
                )
            except Exception as exc:
                log.warning("ElevenLabs WS stream failed, falling back to Sarvam: %s", exc)
                await _ws_batch_fallback(websocket, text_queue, t0)
        else:
            # Primary path: drain queue, batch-synthesize via Sarvam Bulbul
            await _ws_batch_fallback(websocket, text_queue, t0)

    except WebSocketDisconnect:
        pass
    except Exception as exc:
        log.error("tts_stream_ws error: %s", exc)
        try:
            await websocket.send_text(json.dumps({"type": "error", "message": str(exc)}))
        except Exception:
            pass
    finally:
        receive_task.cancel()
        try:
            await receive_task
        except (asyncio.CancelledError, Exception):
            pass


async def _ws_batch_fallback(
    websocket: WebSocket,
    text_queue: asyncio.Queue,
    t0: float,
) -> None:
    """Drain remaining text from queue and batch-synthesize via Sarvam as fallback.

    Returns µ-law 8kHz bytes (converted from L16 PCM via audioop).
    Awaits the sentinel (None) to ensure all tokens have been received.
    """
    import audioop

    # Drain all tokens, waiting for sentinel (None) from receive_loop
    tokens: list[str] = []
    try:
        while True:
            item = await asyncio.wait_for(text_queue.get(), timeout=5.0)
            if item is None:
                break
            tokens.append(item)
    except asyncio.TimeoutError:
        pass

    text = "".join(tokens)
    if not text:
        await websocket.send_text(
            json.dumps({"type": "done", "latency_ms": int((time.time() - t0) * 1000), "engine": "silence"})
        )
        return

    from tts_router.engines.sarvam import SarvamBulbulEngine
    sarvam = SarvamBulbulEngine()
    voice_id = os.getenv("ELEVENLABS_VOICE_ID", "meera")  # use sarvam voice as fallback
    lang = "hi-en"
    try:
        pcm_bytes = await sarvam.synthesize(text, voice_id, lang)
        # Convert L16 PCM 8kHz → µ-law 8kHz
        ulaw_bytes = audioop.lin2ulaw(pcm_bytes, 2)
        # Send in 160-byte chunks (20ms @ 8kHz µ-law)
        chunk_size = 160
        for i in range(0, len(ulaw_bytes), chunk_size):
            await websocket.send_bytes(ulaw_bytes[i:i + chunk_size])
        latency_ms = int((time.time() - t0) * 1000)
        await websocket.send_text(
            json.dumps({"type": "done", "latency_ms": latency_ms, "engine": "sarvam_bulbul_fallback"})
        )
    except Exception as exc:
        log.error("Batch fallback also failed: %s", exc)
        await websocket.send_text(json.dumps({"type": "error", "message": str(exc)}))


@app.websocket("/v1/tts/sarvam/stream")
async def tts_sarvam_stream_ws(websocket: WebSocket) -> None:
    """Sarvam Bulbul:v3 streaming-TTS WebSocket endpoint (flag-gated).

    CONTRACT:
      Client → Server (one text JSON frame per utterance):
        {"text": "<full sentence>", "voice_id": "<id>", "lang": "hi-en"}

      Server → Client (binary frames):
        Raw L16 PCM16 LE 8kHz audio bytes. First chunk arrives ~0.3s after the
        text frame (vs ~2.2s REST). PCM (not µ-law) because the worker's send_audio
        µ-law-encodes itself — emitting µ-law here would double-encode.

      Server → Client (text JSON on completion):
        {"type": "done", "first_chunk_ms": <int>, "total_chunks": <int>, "engine": "sarvam_stream"}

      Server → Client (text JSON on error):
        {"type": "error", "message": "<str>"}

    Feature-flag: TTS_STREAMING_WS=false (default) → returns an error frame so the
    worker uses the safe batch REST path. Only when TTS_STREAMING_WS=true does this
    stream Sarvam WS audio. The engine yields 24kHz L16 PCM; we resample to 8kHz here.
    """
    await websocket.accept()
    if not _TTS_STREAMING_WS:
        await websocket.send_text(
            json.dumps({"type": "error", "message": "streaming_disabled"})
        )
        await websocket.close()
        return

    import numpy as np

    from tts_router.engines.sarvam import SarvamBulbulEngine, SarvamStreamingSession, _STREAM_SAMPLE_RATE

    def _resample_to_8k_pcm(pcm: bytes) -> bytes:
        """PCM16 LE @ _STREAM_SAMPLE_RATE → PCM16 LE 8kHz (numpy linear interp)."""
        samples = np.frombuffer(pcm, dtype="<i2").astype(np.float32)
        if len(samples) == 0:
            return b""
        if _STREAM_SAMPLE_RATE != 8000:
            ratio = 8000.0 / _STREAM_SAMPLE_RATE
            n_out = max(1, int(len(samples) * ratio))
            x_in = np.arange(len(samples), dtype=np.float32)
            x_out = np.linspace(0, len(samples) - 1, n_out)
            samples = np.interp(x_out, x_in, samples)
        return np.clip(samples, -32768, 32767).astype("<i2").tobytes()

    engine = SarvamBulbulEngine()
    # SarvamStreamingSession keeps the Sarvam WS alive across turns via periodic
    # WS-level pings (every 20s), preventing the 408 idle-timeout that caused a
    # per-turn reconnect penalty. On WS failure it reconnects once transparently.
    try:
        async with SarvamStreamingSession(engine) as session:
            # Per-session reuse: loop, synthesising one utterance per inbound text frame
            # until the worker disconnects on hangup.
            while True:
                raw = await websocket.receive_text()
                msg = json.loads(raw)
                text = msg.get("text", "")
                voice_id = msg.get("voice_id", "")
                lang = msg.get("lang", "hi-en")
                if not text:
                    await websocket.send_text(json.dumps(
                        {"type": "done", "first_chunk_ms": 0, "total_chunks": 0, "engine": "silence"}))
                    continue
                t0 = time.time()
                first_chunk_ms = -1
                total_chunks = 0
                try:
                    async for pcm_chunk in session.synthesize(text, voice_id, lang):
                        pcm8k = _resample_to_8k_pcm(pcm_chunk)
                        if not pcm8k:
                            continue
                        if first_chunk_ms < 0:
                            first_chunk_ms = int((time.time() - t0) * 1000)
                        total_chunks += 1
                        await websocket.send_bytes(pcm8k)
                except Exception as synth_exc:
                    # Session synthesis failed (both attempts exhausted) — fall back to
                    # per-call batch REST synthesize so the caller still hears audio.
                    log.warning("tts_sarvam_stream_ws session failed, falling back to REST: %s", synth_exc)
                    pcm = await engine.synthesize(text, voice_id, lang)
                    if pcm:
                        await websocket.send_bytes(pcm)
                        first_chunk_ms = int((time.time() - t0) * 1000)
                        total_chunks = 1
                log.info("[diag] phase=tts_stream first_chunk_ms=%d total_chunks=%d",
                         first_chunk_ms, total_chunks)
                await websocket.send_text(
                    json.dumps({"type": "done", "first_chunk_ms": first_chunk_ms,
                                "total_chunks": total_chunks, "engine": "sarvam_stream"})
                )
    except WebSocketDisconnect:
        pass
    except Exception as exc:
        log.error("tts_sarvam_stream_ws error: %s", exc)
        try:
            await websocket.send_text(json.dumps({"type": "error", "message": str(exc)}))
        except Exception:
            pass
