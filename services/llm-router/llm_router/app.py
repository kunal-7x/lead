from __future__ import annotations

import os

import redis.asyncio as aioredis
from fastapi import FastAPI
from fastapi.responses import JSONResponse, StreamingResponse

from llm_router.backends.groq import GroqLlamaBackend
from llm_router.backends.sarvam import SarvamLLMBackend
from llm_router.backends.vllm import VLLMBackend
from llm_router.backends.openai_backend import (
    OpenAIBackend, AnthropicBackend, OpenRouterBackend, GoogleGeminiBackend
)
from llm_router.claim_control import HttpClaimControl
from llm_router.kb_client import HttpKbRetriever
from llm_router.models import HealthResponse, LLMRequest
from llm_router.router import LLMRouter
from llm_router.switcher import ModelSwitcher

app = FastAPI(title="llm-router", version="0.1.0")
_router: LLMRouter | None = None


def _build_router() -> LLMRouter:
    rdb = aioredis.from_url(os.getenv("REDIS_URL", "redis://localhost:6379"))
    switcher = ModelSwitcher(rdb)
    kb = HttpKbRetriever()
    backends = {
        "groq_llama": GroqLlamaBackend(),
        "sarvam_llm": SarvamLLMBackend(),
        "qwen3_32b": VLLMBackend("qwen3_32b"),
        "llama3_70b": VLLMBackend("llama3_70b"),
        "mistral_7b": VLLMBackend("mistral_7b"),
        "openai_gpt4o": OpenAIBackend(),
        "anthropic_claude": AnthropicBackend(),
        "openrouter": OpenRouterBackend(),
        "google_gemini": GoogleGeminiBackend(),
    }
    return LLMRouter(backends, switcher, kb, HttpClaimControl())


@app.on_event("startup")
async def startup() -> None:
    global _router
    _router = _build_router()


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok"}


@app.get("/v1/llm/models")
async def list_models() -> dict:
    from llm_router.switcher import VALID_MODELS, DEFAULT_MODEL
    return {"models": sorted(VALID_MODELS), "default": DEFAULT_MODEL}


@app.get("/v1/health/engines", response_model=HealthResponse)
async def engine_health() -> HealthResponse:
    assert _router is not None
    engines = await _router.engine_health()
    return HealthResponse(engines=engines)


@app.post("/v1/llm/generate")
async def generate(req: LLMRequest) -> JSONResponse:
    assert _router is not None
    result = await _router.generate(req)
    return JSONResponse(result.model_dump())


@app.post("/v1/llm/generate/stream")
async def generate_stream(req: LLMRequest) -> StreamingResponse:
    """Stream LLM tokens as Server-Sent Events (text/event-stream).

    Token events:  data: {"token":"<piece>","done":false}\\n\\n
    Final event:   data: {"token":"","done":true,"summary":"<>","next_action":"<>"}\\n\\n
    """
    assert _router is not None
    return StreamingResponse(
        _router.generate_stream(req),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )


@app.post("/v1/llm/generate/stream_text")
async def generate_stream_text(req: LLMRequest) -> StreamingResponse:
    """Stream PLAIN TEXT spoken reply tokens as SSE — NO json_object mode.

    First-token latency ~0.8s (vs ~3.4s for /stream which uses json_object mode).
    Use this endpoint for the SPEECH path; call /generate in parallel for
    structured metadata (next_action, summary, lead fields).

    Token events:  data: {"token":"<piece>","done":false}\\n\\n
    Final event:   data: {"token":"","done":true}\\n\\n
    """
    assert _router is not None
    return StreamingResponse(
        _router.generate_stream_text(req),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )
