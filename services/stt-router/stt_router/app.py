from __future__ import annotations

import json
import os

import redis.asyncio as aioredis
from fastapi import FastAPI, WebSocket, WebSocketDisconnect, UploadFile, File
from fastapi.responses import JSONResponse

from stt_router.engines.sarvam import SarvamEngine
from stt_router.engines.indicconformer import IndicConformerEngine
from stt_router.engines.faster_whisper import FasterWhisperEngine
from stt_router.engines.groq_whisper import GroqWhisperEngine
from stt_router.models import STTRequest, HealthResponse
from stt_router.router import STTRouter
from stt_router.switcher import EngineSwitcher

app = FastAPI(title="stt-router", version="0.1.0")

_router: STTRouter | None = None


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
    global _router
    _router = _build_router()


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
async def stt_stream(websocket: WebSocket) -> None:
    """Bidirectional WebSocket STT stream.

    First message: JSON header {lang, session_id, tenant_id}
    Subsequent messages: binary L16 PCM frames (8kHz or 16kHz)
    Server sends: JSON STTResult for each transcribed chunk
    """
    assert _router is not None
    await websocket.accept()
    req: STTRequest | None = None

    try:
        # First message must be JSON header
        header_raw = await websocket.receive_text()
        req = STTRequest.model_validate_json(header_raw)

        # Stream binary audio frames
        while True:
            data = await websocket.receive_bytes()
            result = await _router.transcribe(data, req.lang, req.session_id, req.tenant_id)
            await websocket.send_text(result.model_dump_json())

    except WebSocketDisconnect:
        pass
    except Exception as exc:
        await websocket.send_text(json.dumps({"error": str(exc)}))
        await websocket.close()
