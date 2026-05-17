from __future__ import annotations

import os

import redis.asyncio as aioredis
from fastapi import FastAPI
from fastapi.responses import JSONResponse, Response

from tts_router.cache import RedisAudioCache
from tts_router.engines.sarvam import SarvamBulbulEngine
from tts_router.engines.kokoro import KokoroEngine, IndicParlerEngine, IndicF5Engine, ElevenLabsEngine
from tts_router.models import TTSRequest, WarmRequest
from tts_router.router import TTSRouter
from tts_router.switcher import EngineSwitcher

app = FastAPI(title="tts-router", version="0.1.0")
_router: TTSRouter | None = None


def _build_router() -> TTSRouter:
    rdb = aioredis.from_url(os.getenv("REDIS_URL", "redis://localhost:6379"))
    switcher = EngineSwitcher(rdb)
    cache = RedisAudioCache(rdb)
    engines = {
        "sarvam_bulbul": SarvamBulbulEngine(),
        "kokoro": KokoroEngine(),
        "indic_parler": IndicParlerEngine(),
        "indicf5": IndicF5Engine(),
        "elevenlabs": ElevenLabsEngine(),
    }
    return TTSRouter(engines, switcher, cache)


@app.on_event("startup")
async def startup() -> None:
    global _router
    _router = _build_router()


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok"}


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
