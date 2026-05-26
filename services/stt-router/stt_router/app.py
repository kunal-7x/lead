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
    Subsequent messages:
      - binary L16 PCM frames (8kHz or 16kHz) — accumulated into a buffer
      - TEXT {"type":"flush"} — finalize and transcribe all buffered audio,
        return an STTResult JSON, then reset the buffer for the next utterance
    Server sends: JSON STTResult for each transcribed chunk / flush
    """
    assert _router is not None
    await websocket.accept()
    req: STTRequest | None = None
    audio_buffer: bytearray = bytearray()

    try:
        # First message must be JSON header
        header_raw = await websocket.receive_text()
        req = STTRequest.model_validate_json(header_raw)

        # Stream binary audio frames or control messages
        while True:
            # Receive either binary audio or text control message
            msg = await websocket.receive()

            if msg["type"] == "websocket.receive":
                if "bytes" in msg and msg["bytes"] is not None:
                    # Binary frame: accumulate audio
                    audio_buffer.extend(msg["bytes"])
                    # Auto-transcribe each frame (existing behaviour) so the
                    # caller can still consume incremental results without flush
                    result = await _router.transcribe(
                        bytes(msg["bytes"]), req.lang, req.session_id, req.tenant_id
                    )
                    await websocket.send_text(result.model_dump_json())

                elif "text" in msg and msg["text"] is not None:
                    # Text control message
                    try:
                        ctrl = json.loads(msg["text"])
                    except Exception:
                        ctrl = {}

                    if ctrl.get("type") == "flush":
                        # Transcribe the entire accumulated buffer, return best result
                        if audio_buffer:
                            result = await _router.transcribe(
                                bytes(audio_buffer), req.lang, req.session_id, req.tenant_id
                            )
                        else:
                            # Nothing buffered — return an empty result
                            from stt_router.models import STTResult
                            import time as _time
                            _ts = _time.time()
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
                        # Reset buffer for the next utterance
                        audio_buffer = bytearray()
                    # Unknown text messages are silently ignored

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
