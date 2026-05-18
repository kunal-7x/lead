from __future__ import annotations

import asyncio
import base64
import json
import os

import redis.asyncio as aioredis
from fastapi import FastAPI, WebSocket, WebSocketDisconnect

from voice_agent.agent import AgentLoop
from voice_agent.clients import HttpSTTClient, HttpLLMClient, HttpGuardrailClient, HttpTTSClient
from voice_agent.demo_runtime import DemoEventPublisher, DemoTurnStore
from voice_agent.models import SessionContext
from voice_agent.vad import SileroVAD

app = FastAPI(title="voice-agent-worker", version="0.1.0")

_redis: aioredis.Redis | None = None
_SESSION_TTL = 1800  # 30 minutes


@app.on_event("startup")
async def startup() -> None:
    global _redis
    _redis = aioredis.from_url(os.getenv("REDIS_URL", "redis://localhost:6379"))


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok"}


@app.websocket("/ws/audio/{session_id}")
async def audio_ws(websocket: WebSocket, session_id: str) -> None:
    await websocket.accept()
    ctx = await _load_context(session_id)

    stt = HttpSTTClient()
    llm = HttpLLMClient()
    guardrail = HttpGuardrailClient()
    tts = HttpTTSClient()
    publisher = DemoEventPublisher()
    store = DemoTurnStore()
    vad = SileroVAD()

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


async def _load_context(session_id: str) -> SessionContext:
    """Load session context from Redis (written by telephony-adapter on call start)."""
    if _redis is None:
        return SessionContext(session_id=session_id, tenant_id="unknown")
    raw = await _redis.get(f"call:session:{session_id}")
    if raw is None:
        return SessionContext(session_id=session_id, tenant_id="unknown")
    data = json.loads(raw)
    return SessionContext(session_id=session_id, **{
        k: v for k, v in data.items()
        if k in SessionContext.__dataclass_fields__
        and k != "session_id"
    })
