from __future__ import annotations

import asyncio
import json
import time

from llm_router.backends.base import LLMBackend
from llm_router.kb_client import KbRetriever
from llm_router.models import BrainOutput, LLMRequest, LLMResponse, FALLBACK_BRAIN
from llm_router.switcher import ModelSwitcher

_TIMEOUT_S = 20.0


class LLMRouter:
    """Routes LLM requests through backends with RAG, schema enforcement, and failover.

    Flow per request:
    1. Read active model from Redis (per-tenant override → global → default groq_llama)
    2. Retrieve KB chunks from knowledge service (always, before LLM call)
    3. Call active backend with KB context
    4. Validate BrainOutput schema
    5. On failure/timeout → try next available backend
    6. All fail → return FALLBACK_BRAIN (needs_human_review)
    """

    def __init__(
        self,
        backends: dict[str, LLMBackend],
        switcher: ModelSwitcher,
        kb: KbRetriever,
    ) -> None:
        self._backends = backends
        self._switcher = switcher
        self._kb = kb

    async def generate(self, req: LLMRequest) -> LLMResponse:
        t0 = time.time()

        # Always retrieve KB before LLM call
        chunks = await self._kb.retrieve(req.project_id, req.user_turn, req.lang)
        req = req.model_copy(update={"kb_chunks": chunks})
        kb_context = _format_kb(chunks)

        active = await self._switcher.active_model(req.tenant_id)
        chain = _build_chain(active, list(self._backends.keys()))

        last_err: Exception | None = None
        for model_name in chain:
            backend = self._backends.get(model_name)
            if backend is None:
                continue
            try:
                brain, pt, ct = await asyncio.wait_for(
                    backend.generate(req, kb_context), timeout=_TIMEOUT_S
                )
                return LLMResponse(
                    brain=brain,
                    model_used=model_name,
                    prompt_tokens=pt,
                    completion_tokens=ct,
                    latency_ms=int((time.time() - t0) * 1000),
                )
            except Exception as exc:
                last_err = exc
                continue

        # All backends failed — safe fallback
        return LLMResponse(
            brain=FALLBACK_BRAIN,
            model_used="fallback",
            latency_ms=int((time.time() - t0) * 1000),
        )


def _format_kb(chunks) -> str:
    if not chunks:
        return "(no KB context)"
    return "\n".join(f"- {c.text}" for c in chunks)


def _build_chain(active: str, available: list[str]) -> list[str]:
    """Active model first, then remaining in order."""
    rest = [n for n in available if n != active]
    return [active] + rest
