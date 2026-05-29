from __future__ import annotations

import asyncio
import base64
import json
import os

import httpx
import redis.asyncio as aioredis
from fastapi import FastAPI, WebSocket, WebSocketDisconnect

from voice_agent.agent import AgentLoop
from voice_agent.clients import (
    HttpSTTClient, HttpLLMClient, HttpGuardrailClient, HttpTTSClient, _POOL_LIMITS,
)
from voice_agent.demo_runtime import DemoEventPublisher, DemoTurnStore
from voice_agent.models import SessionContext
from voice_agent.vad import SileroVAD
from voice_agent.vobiz_media import run_vobiz_bridge

app = FastAPI(title="voice-agent-worker", version="0.1.0")

_redis: aioredis.Redis | None = None
_SESSION_TTL = 1800  # 30 minutes

# Process-wide shared router clients. Created once at startup (after env is loaded)
# and reused across every call so concurrent calls share TCP connection pools
# instead of each spinning up a fresh pool. STT/LLM/Guardrail are stateless (they
# only hold an httpx pool) so the instances are shared directly. TTS holds a
# per-session streaming WS, so we share only its underlying httpx pool and build a
# fresh HttpTTSClient per call around that shared pool.
_stt: HttpSTTClient | None = None
_llm: HttpLLMClient | None = None
_guardrail: HttpGuardrailClient | None = None
_tts_http: httpx.AsyncClient | None = None


@app.on_event("startup")
async def startup() -> None:
    global _redis, _stt, _llm, _guardrail, _tts_http
    _redis = aioredis.from_url(os.getenv("REDIS_URL", "redis://localhost:6379"))
    _stt = HttpSTTClient()
    _llm = HttpLLMClient()
    _guardrail = HttpGuardrailClient()
    _tts_http = httpx.AsyncClient(timeout=10.0, limits=_POOL_LIMITS)


@app.on_event("shutdown")
async def shutdown() -> None:
    for c in (_stt, _llm, _guardrail):
        if c is not None:
            await c.aclose()
    if _tts_http is not None:
        await _tts_http.aclose()


def _make_shared_services() -> tuple:
    """Return (stt, llm, guardrail, tts, publisher, store, vad) for one call.

    STT/LLM/Guardrail are the shared process-wide singletons. TTS is a per-call
    instance that reuses the shared httpx pool but keeps its own per-session
    streaming WS. publisher/store/vad stay per-call as before.
    """
    return (
        _stt,
        _llm,
        _guardrail,
        HttpTTSClient(shared_http_client=_tts_http),
        DemoEventPublisher(),
        DemoTurnStore(),
        SileroVAD(),
    )


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok"}


@app.websocket("/ws/audio/{session_id}")
async def audio_ws(websocket: WebSocket, session_id: str) -> None:
    await websocket.accept()
    ctx = await _load_context(session_id)

    stt, llm, guardrail, tts, publisher, store, vad = _make_shared_services()

    async def audio_source():
        try:
            while True:
                data = await websocket.receive()
                if "bytes" in data:
                    yield data["bytes"]
                elif "text" in data:
                    msg = json.loads(data["text"])
                    if msg.get("type") == "stop":
                        return
        except WebSocketDisconnect:
            return

    async def send_audio(audio: bytes) -> None:
        b64 = base64.b64encode(audio).decode()
        await websocket.send_text(json.dumps({"type": "playback", "audio": b64}))

    async def send_json(msg: dict) -> None:
        await websocket.send_text(json.dumps(msg))

    loop = AgentLoop(ctx, stt, llm, guardrail, tts, publisher, store, vad)
    try:
        await loop.run(audio_source(), send_audio, send_json)
    except WebSocketDisconnect:
        pass
    finally:
        await send_json({"type": "call_complete"})
        # Close the per-call TTS streaming WS (shared httpx pool is left intact).
        try:
            await tts.aclose()
        except Exception:  # noqa: BLE001
            pass


@app.websocket("/ws/vobiz/{internal_call_id}")
async def vobiz_ws(websocket: WebSocket, internal_call_id: str) -> None:
    """Vobiz µ-law media-stream bridge — adapts Vobiz JSON protocol to AgentLoop."""
    await websocket.accept()
    try:
        await run_vobiz_bridge(
            websocket, internal_call_id,
            _make_services_fn=_make_shared_services,
        )
    except WebSocketDisconnect:
        pass


async def _load_context(session_id: str) -> SessionContext:
    """Load session context from Redis (written by telephony-adapter on call start)."""
    if _redis is None:
        return SessionContext(session_id=session_id, tenant_id="unknown")
    raw = await _redis.get(f"call:session:{session_id}")
    if raw is None:
        return SessionContext(session_id=session_id, tenant_id="unknown")
    data = json.loads(raw)
    ctx = SessionContext(session_id=session_id, **{
        k: v for k, v in data.items()
        if k in SessionContext.__dataclass_fields__
        and k != "session_id"
    })
    # Operational override: force premium TTS (e.g. ElevenLabs) when the primary
    # provider is unavailable/out of credits. Set TTS_PREMIUM=1 on the worker.
    if os.getenv("TTS_PREMIUM") == "1":
        ctx.tts_premium = True
    return ctx
