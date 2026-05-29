"""Tests for concurrency guard (semaphore) and backoff retry logic in STTRouter."""
from __future__ import annotations

import asyncio

import pytest

import stt_router.router as _router_mod
from stt_router.engines.base import STTEngine
from stt_router.models import STTResult
from stt_router.router import STTRouter
from tests.fakes.fake_sarvam import FakeSarvamEngine
from tests.fakes.fake_indicconformer import FakeIndicConformerEngine

_AUDIO = b"\x00\x01" * 320


class _429Engine(STTEngine):
    """Engine that raises a 429-like error on the first N calls, then succeeds."""

    name = "sarvam"

    def __init__(self, fail_times: int = 1) -> None:
        self._fail_times = fail_times
        self.call_count = 0
        self._fake = FakeSarvamEngine(transcript="ok", confidence=0.92)

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        self.call_count += 1
        if self.call_count <= self._fail_times:
            raise RuntimeError("upstream 429 rate limit exceeded")
        return await self._fake.transcribe(audio, lang, session_id)

    async def health_check(self) -> bool:
        return True


async def test_semaphore_limits_concurrency(switcher):
    """Semaphore with size=2 allows at most 2 simultaneous engine calls."""
    # Save and override module-level semaphore to size 2
    orig_sem = _router_mod._semaphore
    orig_max = _router_mod._MAX_CONCURRENT
    _router_mod._semaphore = asyncio.Semaphore(2)
    _router_mod._MAX_CONCURRENT = 2

    concurrent_peak = 0
    current = 0

    class _CountingEngine(STTEngine):
        name = "sarvam"

        async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
            nonlocal concurrent_peak, current
            current += 1
            concurrent_peak = max(concurrent_peak, current)
            await asyncio.sleep(0.05)  # hold the slot briefly
            current -= 1
            fake = FakeSarvamEngine(transcript="x", confidence=0.9)
            return await fake.transcribe(audio, lang, session_id)

        async def health_check(self) -> bool:
            return True

    router = STTRouter({"sarvam": _CountingEngine()}, switcher)

    # Fire 5 concurrent transcribe calls
    tasks = [
        asyncio.create_task(router.transcribe(_AUDIO, "hi-en", f"s{i}", "t1"))
        for i in range(5)
    ]
    await asyncio.gather(*tasks)

    _router_mod._semaphore = orig_sem
    _router_mod._MAX_CONCURRENT = orig_max

    assert concurrent_peak <= 2, f"Expected ≤2 concurrent calls, got {concurrent_peak}"


async def test_backoff_retries_on_429(switcher):
    """Engine returning 429-like error is retried up to 3 times before succeeding."""
    engine = _429Engine(fail_times=2)  # fail twice, succeed on 3rd call
    router = STTRouter({"sarvam": engine}, switcher)

    result = await router.transcribe(_AUDIO, "hi-en", "sess-retry", "t1")

    assert result.text == "ok"
    assert engine.call_count == 3  # 2 retries + 1 success


async def test_backoff_exhausted_falls_through_to_next_engine(switcher):
    """All retries exhausted → router falls through to next engine in chain."""
    bad_engine = _429Engine(fail_times=10)  # more failures than retries
    bad_engine.name = "sarvam"
    good_engine = FakeIndicConformerEngine(transcript="fallback ok", confidence=0.88)

    router = STTRouter(
        {"sarvam": bad_engine, "indicconformer": good_engine},
        switcher,
    )

    result = await router.transcribe(_AUDIO, "hi-en", "sess-exhaust", "t1")

    # Should have fallen through to indicconformer
    assert result.engine_used == "indicconformer"
    assert result.text == "fallback ok"
