"""Tests for concurrency guard (semaphore) and backoff retry logic in TTSRouter."""
from __future__ import annotations

import asyncio

import tts_router.router as _router_mod
from tts_router.cache import FakeAudioCache
from tts_router.engines.base import TTSEngine
from tts_router.models import TTSRequest, VoiceInfo
from tts_router.router import TTSRouter
from tests.fakes.fake_engines import FakeSarvamTTS, FakeKokoroTTS, _make_pcm


class _429Engine(TTSEngine):
    """Engine that raises a 429-like error on first N calls, then succeeds."""

    name = "sarvam_bulbul"
    premium_only = False

    def __init__(self, fail_times: int = 1) -> None:
        self._fail_times = fail_times
        self.call_count = 0

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        self.call_count += 1
        if self.call_count <= self._fail_times:
            raise RuntimeError("upstream 429 rate limit exceeded")
        return _make_pcm(800)

    async def health_check(self) -> bool:
        return True

    def voices(self) -> list[VoiceInfo]:
        return []


def _make_req(**kwargs) -> TTSRequest:
    defaults = {
        "text": "Namaste",
        "lang": "hi-en",
        "voice_id": "meera",
        "tenant_id": "tenant-1",
        "session_id": "sess-1",
    }
    defaults.update(kwargs)
    return TTSRequest(**defaults)


async def test_semaphore_limits_concurrency(switcher):
    """Semaphore with size=2 allows at most 2 simultaneous engine calls."""
    orig_sem = _router_mod._semaphore
    orig_max = _router_mod._MAX_CONCURRENT
    _router_mod._semaphore = asyncio.Semaphore(2)
    _router_mod._MAX_CONCURRENT = 2

    concurrent_peak = 0
    current = 0

    class _CountingEngine(TTSEngine):
        name = "sarvam_bulbul"
        premium_only = False

        async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
            nonlocal concurrent_peak, current
            current += 1
            concurrent_peak = max(concurrent_peak, current)
            await asyncio.sleep(0.05)
            current -= 1
            return _make_pcm(800)

        async def health_check(self) -> bool:
            return True

        def voices(self) -> list[VoiceInfo]:
            return []

    router = TTSRouter({"sarvam_bulbul": _CountingEngine()}, switcher, FakeAudioCache())

    tasks = [
        asyncio.create_task(router.synthesize(_make_req(session_id=f"s{i}")))
        for i in range(5)
    ]
    await asyncio.gather(*tasks)

    _router_mod._semaphore = orig_sem
    _router_mod._MAX_CONCURRENT = orig_max

    assert concurrent_peak <= 2, f"Expected ≤2 concurrent calls, got {concurrent_peak}"


async def test_backoff_retries_on_429(switcher):
    """Engine returning 429-like error is retried up to 3 times before succeeding."""
    engine = _429Engine(fail_times=2)
    router = TTSRouter({"sarvam_bulbul": engine}, switcher, FakeAudioCache())

    result = await router.synthesize(_make_req())

    assert result.tier_used == "sarvam_bulbul"
    assert engine.call_count == 3


async def test_backoff_exhausted_falls_through_to_next_engine(switcher):
    """All retries exhausted → router falls through to next engine in chain."""
    bad = _429Engine(fail_times=10)
    good = FakeKokoroTTS()

    router = TTSRouter(
        {"sarvam_bulbul": bad, "kokoro": good},
        switcher,
        FakeAudioCache(),
    )

    result = await router.synthesize(_make_req())

    assert result.tier_used == "kokoro"
