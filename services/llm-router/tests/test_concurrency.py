"""Tests for concurrency guard (semaphore) and backoff retry logic in LLMRouter."""
from __future__ import annotations

import asyncio

import llm_router.router as _router_mod
from llm_router.kb_client import FakeKbRetriever
from llm_router.models import BrainOutput, LLMRequest, FALLBACK_BRAIN
from llm_router.router import LLMRouter
from tests.conftest import make_req
from tests.fakes.fake_backends import FakeBackend, FakeGroqBackend

_DEFAULT_BRAIN = BrainOutput(
    reply="Test reply",
    lead_status="warm",
    lead_score=70,
    next_action="qualify",
    risk_level="safe",
    confidence=0.9,
    summary="Test",
)


class _429Backend(FakeBackend):
    """Backend that raises a 429-like error on the first N calls, then succeeds."""

    def __init__(self, fail_times: int = 1, **kwargs) -> None:
        super().__init__(**kwargs)
        self._fail_times = fail_times

    async def generate(self, req: LLMRequest, kb_context: str):
        self.call_count += 1
        if self.call_count <= self._fail_times:
            raise RuntimeError("upstream 429 rate limit exceeded")
        return _DEFAULT_BRAIN, 100, 50


async def test_semaphore_limits_concurrency(switcher):
    """Semaphore with size=2 allows at most 2 simultaneous backend calls."""
    orig_sem = _router_mod._semaphore
    orig_max = _router_mod._MAX_CONCURRENT
    _router_mod._semaphore = asyncio.Semaphore(2)
    _router_mod._MAX_CONCURRENT = 2

    concurrent_peak = 0
    current = 0

    class _CountingBackend(FakeBackend):
        async def generate(self, req, kb_context):
            nonlocal concurrent_peak, current
            current += 1
            concurrent_peak = max(concurrent_peak, current)
            await asyncio.sleep(0.05)
            current -= 1
            return _DEFAULT_BRAIN, 100, 50

    router = LLMRouter(
        {"groq_llama": _CountingBackend(name="groq_llama")},
        switcher,
        FakeKbRetriever(),
    )

    tasks = [
        asyncio.create_task(router.generate(make_req(session_id=f"s{i}")))
        for i in range(5)
    ]
    await asyncio.gather(*tasks)

    _router_mod._semaphore = orig_sem
    _router_mod._MAX_CONCURRENT = orig_max

    assert concurrent_peak <= 2, f"Expected ≤2 concurrent calls, got {concurrent_peak}"


async def test_backoff_retries_on_429(switcher):
    """Backend returning 429-like error is retried up to 3 times before succeeding."""
    backend = _429Backend(name="groq_llama", fail_times=2)
    router = LLMRouter(
        {"groq_llama": backend},
        switcher,
        FakeKbRetriever(),
    )

    result = await router.generate(make_req())

    assert result.model_used == "groq_llama"
    assert backend.call_count == 3


async def test_backoff_exhausted_falls_through_to_next_backend(switcher):
    """All retries exhausted → router falls through to next backend in chain."""
    bad = _429Backend(name="groq_llama", fail_times=10)
    good = FakeGroqBackend()
    good.name = "cerebras_llama"

    router = LLMRouter(
        {"groq_llama": bad, "cerebras_llama": good},
        switcher,
        FakeKbRetriever(),
    )

    result = await router.generate(make_req())

    assert result.model_used == "cerebras_llama"
