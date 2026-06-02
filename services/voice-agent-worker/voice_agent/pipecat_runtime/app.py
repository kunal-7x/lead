"""FastAPI app for the Pipecat runtime (parallel to voice_agent.app).

Endpoints:
  * GET  /pipecat/answer        → Vobiz <Stream> XML pointing at the WS below.
  * WS   /pipecat/ws/{call_id}  → accept WS, read the Vobiz ``start`` frame, load
                                  the Redis session, build the serializer +
                                  transport + pipeline, and run the PipelineTask.
  * GET  /pipecat/healthz       → liveness.

Bind 127.0.0.1:PIPECAT_PORT (nginx fronts it — NO new public port). Redis session
load + NATS publisher wiring reuse the existing worker's logic/utilities verbatim
(``app._load_context`` semantics, ``events_nats.NATSEventPublisher``).
"""

from __future__ import annotations

import json
import logging
import os

import redis.asyncio as aioredis
from fastapi import FastAPI, Request, Response, WebSocket, WebSocketDisconnect

from voice_agent.events_nats import NATSEventPublisher
from voice_agent.models import SessionContext
from voice_agent.pipecat_runtime.config import PipecatSettings
from voice_agent.pipecat_runtime.xml import build_answer_xml

logger = logging.getLogger(__name__)

app = FastAPI(title="voice-agent-worker-pipecat", version="0.1.0")

_settings: PipecatSettings = PipecatSettings.from_env()
_redis: "aioredis.Redis | None" = None
# Process-wide NATS publisher, shared by every call (one NATS connection).
_nats_publisher: NATSEventPublisher | None = None


@app.on_event("startup")
async def startup() -> None:
    global _redis, _nats_publisher, _settings
    _settings = PipecatSettings.from_env()
    _redis = aioredis.from_url(_settings.redis_url)
    if _settings.event_publisher == "nats":
        _nats_publisher = NATSEventPublisher(nats_url=_settings.nats_url)
    logger.info(
        "pipecat runtime startup (flag=%s, port=%d, publisher=%s)",
        _settings.voice_runtime, _settings.pipecat_port, _settings.event_publisher,
    )


@app.on_event("shutdown")
async def shutdown() -> None:
    if _nats_publisher is not None:
        await _nats_publisher.aclose()
    if _redis is not None:
        await _redis.aclose()


@app.get("/pipecat/healthz")
async def healthz() -> dict:
    return {"status": "ok", "runtime": "pipecat", "flag": _settings.voice_runtime}


@app.api_route("/pipecat/answer", methods=["GET", "POST"])
async def answer(call_id: str = "", request: Request = None) -> Response:
    """Return the Vobiz Answer XML. Accepts GET and POST (Vobiz POSTs to answer_url)."""
    # Vobiz POSTs the call details as form-data; extract call_uuid if call_id not in query.
    if not call_id and request is not None:
        try:
            form = await request.form()
            call_id = (
                form.get("CallUUID") or form.get("call_id") or
                form.get("From") or ""
            )
        except Exception:  # noqa: BLE001
            pass
    xml = build_answer_xml(_settings.public_ws_base, call_id)
    return Response(content=xml, media_type="application/xml")


async def _load_context(session_id: str) -> SessionContext:
    """Load SessionContext from Redis ``call:session:{id}``.

    Mirrors voice_agent.app._load_context EXACTLY: same key, same field filter,
    same TTS_PREMIUM override. Reused so the telephony-adapter handoff is identical.
    """
    if _redis is None:
        return SessionContext(session_id=session_id, tenant_id="unknown")
    raw = await _redis.get(f"call:session:{session_id}")
    if raw is None:
        return SessionContext(session_id=session_id, tenant_id="unknown")
    data = json.loads(raw)
    ctx = SessionContext(session_id=session_id, **{
        k: v for k, v in data.items()
        if k in SessionContext.__dataclass_fields__ and k != "session_id"
    })
    # Operational override: force premium TTS when the primary provider is down.
    if os.getenv("TTS_PREMIUM") == "1":
        ctx.tts_premium = True
    return ctx


def _get_publisher():
    """Return the shared event publisher (NATS in prod, demo otherwise)."""
    if _settings.event_publisher == "nats" and _nats_publisher is not None:
        return _nats_publisher
    # Demo fallback: lazy import so the demo stub stays optional.
    from voice_agent.demo_runtime import DemoEventPublisher
    return DemoEventPublisher()


def _read_start_frame(text: str) -> dict:
    """Parse a Vobiz ``start`` control frame's JSON, tolerating noise.

    Vobiz sends a JSON text frame at stream open containing the stream/call ids
    and media format. We only need the ids to construct the serializer; the
    serializer itself parses subsequent ``media`` frames.
    """
    try:
        return json.loads(text)
    except (ValueError, TypeError):
        return {}


@app.websocket("/pipecat/ws/{call_id}")
async def pipecat_ws(websocket: WebSocket, call_id: str) -> None:
    """Vobiz media-stream WS → Pipecat pipeline.

    Flow: accept WS → read the first ``start`` frame (stream_id) → load Redis
    session → build_pipeline(ctx, websocket, ...) → run the PipelineTask until the
    call ends or the socket drops.
    """
    await websocket.accept()

    # First inbound text frame is the Vobiz "start"/"connected" control frame.
    stream_id = call_id
    session_id = call_id
    try:
        first = await websocket.receive_text()
        start = _read_start_frame(first)
        # Vobiz start frame shapes vary; pull the most specific ids available.
        start_obj = start.get("start", start) if isinstance(start, dict) else {}
        stream_id = (
            start_obj.get("streamId")
            or start_obj.get("stream_id")
            or start.get("stream_id")
            or call_id
        )
        # The Redis session is keyed by the internal session/call id the
        # telephony-adapter wrote; prefer an explicit custom parameter if present.
        session_id = (
            start_obj.get("callId")
            or start_obj.get("call_id")
            or start.get("session_id")
            or call_id
        )
    except WebSocketDisconnect:
        return
    except Exception as exc:  # noqa: BLE001
        logger.warning("pipecat_ws start-frame read failed (%r); using path ids", exc)

    ctx = await _load_context(session_id)
    publisher = _get_publisher()

    # Lazy import: keeps the heavy pipecat stack out of import time (and lets the
    # scaffold/tests import this module without pipecat installed).
    from voice_agent.pipecat_runtime.pipeline import build_pipeline

    # build_pipeline returns (PipelineWorker, WorkerRunner) in pipecat 1.3
    worker = None
    try:
        worker, runner = await build_pipeline(
            ctx=ctx,
            websocket=websocket,
            settings=_settings,
            publisher=publisher,
            stream_id=stream_id,
            call_id=call_id,
        )
        await runner.run(worker)
    except WebSocketDisconnect:
        logger.info("pipecat_ws disconnected call_id=%s", call_id)
    except Exception:  # noqa: BLE001
        logger.exception("pipecat_ws pipeline error call_id=%s", call_id)
    finally:
        if worker is not None:
            try:
                await worker.cancel()
            except Exception:  # noqa: BLE001
                pass


def main() -> None:
    """uvicorn entrypoint: bind 127.0.0.1:PIPECAT_PORT (nginx fronts it)."""
    import uvicorn

    settings = PipecatSettings.from_env()
    uvicorn.run(
        "voice_agent.pipecat_runtime.app:app",
        host=settings.pipecat_host,
        port=settings.pipecat_port,
        log_level="info",
    )


if __name__ == "__main__":
    main()
