from __future__ import annotations

import asyncio
import json

from llm_router.backends.base import LLMBackend
from llm_router.models import BrainOutput, LLMRequest, FALLBACK_BRAIN

_DEFAULT_BRAIN = BrainOutput(
    reply="Haan ji, 2BHK available hai 60 lakh mein.",
    lead_status="warm",
    lead_score=65,
    next_action="qualify",
    risk_level="safe",
    confidence=0.88,
    summary="Lead interested in 2BHK at 60L",
)


class FakeBackend(LLMBackend):
    """Configurable fake LLM backend for tests."""

    def __init__(
        self,
        name: str = "groq_llama",
        brain: BrainOutput | None = None,
        timeout: bool = False,
        raise_error: bool = False,
        healthy: bool = True,
    ) -> None:
        self.name = name
        self._brain = brain or _DEFAULT_BRAIN
        self._timeout = timeout
        self._raise_error = raise_error
        self._healthy = healthy
        self.call_count = 0

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        self.call_count += 1
        if self._timeout:
            await asyncio.sleep(60)
        if self._raise_error:
            raise ValueError("Fake backend error")
        return self._brain, 100, 50

    async def health_check(self) -> bool:
        return self._healthy


class FakeVLLMBackend(FakeBackend):
    """Fake vLLM backend — simulates GPU-hosted model."""
    def __init__(self, model_key: str = "qwen3_32b", **kwargs):
        super().__init__(name=model_key, **kwargs)


class FakeGroqBackend(FakeBackend):
    """Fake Groq backend."""
    def __init__(self, **kwargs):
        super().__init__(name="groq_llama", **kwargs)
