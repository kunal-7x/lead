from __future__ import annotations

import asyncio
import json
import time

from llm_router.backends.base import LLMBackend
from llm_router.claim_control import ClaimControl
from llm_router.kb_client import KbRetriever
from llm_router.models import BrainOutput, EngineHealth, LLMRequest, LLMResponse, FALLBACK_BRAIN
from llm_router.switcher import ModelSwitcher

try:
    from evs_common.langfuse_tracer import trace_span
except Exception:  # evs_common optional outside repo
    import contextlib

    @contextlib.asynccontextmanager
    async def trace_span(*a, **kw):  # type: ignore
        yield {"output": None}

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
        claim_control: ClaimControl | None = None,
    ) -> None:
        self._backends = backends
        self._switcher = switcher
        self._kb = kb
        self._claim_control = claim_control

    async def generate(self, req: LLMRequest, trace_id: str | None = None) -> LLMResponse:
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
                async with trace_span(
                    f"llm.{model_name}",
                    trace_id=trace_id,
                    input={"user_turn": req.user_turn, "lang": req.lang},
                    metadata={"tenant_id": req.tenant_id, "session_id": req.session_id},
                ) as span:
                    brain, pt, ct = await asyncio.wait_for(
                        backend.generate(req, kb_context), timeout=_TIMEOUT_S
                    )
                    span["output"] = {"reply": brain.reply, "lead_score": brain.lead_score,
                                      "next_action": brain.next_action}
                brain = await self._apply_claim_control(req, brain)
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

    async def _apply_claim_control(self, req: LLMRequest, brain: BrainOutput) -> BrainOutput:
        if self._claim_control is None or not req.project_id:
            return brain
        result = await self._claim_control.check(
            tenant_id=req.tenant_id,
            project_id=req.project_id,
            session_id=req.session_id,
            channel="voice",
            text=brain.reply,
        )
        if result.ok:
            return brain
        reply = result.rewritten or "Let me check that and get back to you."
        reason = "; ".join(v.reason for v in result.violations) or "claim control blocked reply"
        return brain.model_copy(
            update={
                "reply": reply,
                "risk_level": "risky",
                "next_action": "handover",
                "should_handover_to_human": True,
                "summary": f"{brain.summary} Claim-control block: {reason}",
            }
        )

    async def engine_health(self) -> list[EngineHealth]:
        results = []
        for name, backend in self._backends.items():
            try:
                ok = await asyncio.wait_for(backend.health_check(), timeout=5.0)
            except Exception:
                ok = False
            results.append(EngineHealth(name=name, available=ok))
        return results


def _format_kb(chunks) -> str:
    if not chunks:
        return "(no KB context)"
    return "\n".join(f"- {c.text}" for c in chunks)


def _build_chain(active: str, available: list[str]) -> list[str]:
    """Active model first, then remaining in order."""
    rest = [n for n in available if n != active]
    return [active] + rest
