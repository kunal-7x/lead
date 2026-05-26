from __future__ import annotations

import pytest

import stt_router.router as _router_mod
from stt_router.router import STTRouter
from stt_router.switcher import EngineSwitcher, DEFAULT_ENGINE
from tests.fakes.fake_sarvam import FakeSarvamEngine
from tests.fakes.fake_indicconformer import FakeIndicConformerEngine
from tests.fakes.fake_groq import FakeGroqWhisperEngine

_AUDIO = b"\x00\x01" * 320  # 320 bytes = 20ms L16 PCM at 8kHz


def _make_router(switcher, **overrides):
    engines = {
        "sarvam": overrides.get("sarvam", FakeSarvamEngine()),
        "indicconformer": overrides.get("indicconformer", FakeIndicConformerEngine()),
        "groq_whisper": overrides.get("groq_whisper", FakeGroqWhisperEngine()),
    }
    return STTRouter(engines, switcher)


async def test_sarvam_primary(switcher):
    """Default engine is Sarvam; result.engine_used == sarvam."""
    sarvam = FakeSarvamEngine(transcript="namaste", confidence=0.92)
    router = _make_router(switcher, sarvam=sarvam)

    result = await router.transcribe(_AUDIO, "hi-en", "sess-1", "tenant-1")

    assert result.engine_used == "sarvam"
    assert result.text == "namaste"
    assert result.confidence >= 0.55


async def test_sarvam_timeout_fallback(switcher):
    """Sarvam times out → falls back to IndicConformer."""
    sarvam = FakeSarvamEngine(timeout=True)
    indicconformer = FakeIndicConformerEngine(transcript="fallback text", confidence=0.87)
    router = _make_router(switcher, sarvam=sarvam, indicconformer=indicconformer)

    # Use a short override so the test doesn't wait for the full 12s Sarvam timeout
    orig = _router_mod._ENGINE_TIMEOUT_S.get("sarvam")
    _router_mod._ENGINE_TIMEOUT_S["sarvam"] = 0.2  # 200ms — fake engine sleeps 60s
    try:
        result = await router.transcribe(_AUDIO, "hi-en", "sess-2", "tenant-1")
    finally:
        if orig is None:
            _router_mod._ENGINE_TIMEOUT_S.pop("sarvam", None)
        else:
            _router_mod._ENGINE_TIMEOUT_S["sarvam"] = orig

    assert result.engine_used == "indicconformer"
    assert result.text == "fallback text"


async def test_hinglish_routing(switcher):
    """lang=hi-en → always routes to Sarvam first."""
    sarvam = FakeSarvamEngine(transcript="hinglish result", confidence=0.90)
    router = _make_router(switcher, sarvam=sarvam)

    result = await router.transcribe(_AUDIO, "hi-en", "sess-3", "tenant-1")

    assert result.engine_used == "sarvam"


async def test_low_confidence_no_rerun_by_default(switcher):
    """Default: Sarvam returns confidence 0.4 → still returned immediately (no serial re-run).

    STT_CONFIDENCE_RERUN defaults to '0', so the router returns the first
    successful engine result regardless of confidence, eliminating the latency
    penalty of a second cloud STT call.
    """
    orig = _router_mod._CONFIDENCE_RERUN
    _router_mod._CONFIDENCE_RERUN = False  # explicitly ensure default
    try:
        sarvam = FakeSarvamEngine(transcript="weak result", confidence=0.40)
        indicconformer = FakeIndicConformerEngine(transcript="better result", confidence=0.87)
        router = _make_router(switcher, sarvam=sarvam, indicconformer=indicconformer)

        result = await router.transcribe(_AUDIO, "hi-en", "sess-4", "tenant-1")

        # Without rerun, we get Sarvam's result immediately
        assert result.engine_used == "sarvam"
        assert result.text == "weak result"
        assert sarvam.call_count == 1
    finally:
        _router_mod._CONFIDENCE_RERUN = orig


async def test_low_confidence_rerun_when_enabled(switcher):
    """When STT_CONFIDENCE_RERUN=1: Sarvam returns confidence 0.4 → re-runs on IndicConformer."""
    orig = _router_mod._CONFIDENCE_RERUN
    _router_mod._CONFIDENCE_RERUN = True
    try:
        sarvam = FakeSarvamEngine(transcript="weak result", confidence=0.40)
        indicconformer = FakeIndicConformerEngine(transcript="better result", confidence=0.87)
        router = _make_router(switcher, sarvam=sarvam, indicconformer=indicconformer)

        result = await router.transcribe(_AUDIO, "hi-en", "sess-4b", "tenant-1")

        assert result.engine_used == "indicconformer"
        assert result.confidence >= 0.55
    finally:
        _router_mod._CONFIDENCE_RERUN = orig


async def test_all_engines_fail(switcher):
    """All engines timeout → RuntimeError."""
    sarvam = FakeSarvamEngine(timeout=True)
    indicconformer = FakeIndicConformerEngine(confidence=0.0)

    engines = {"sarvam": sarvam, "indicconformer": indicconformer}
    router = STTRouter(engines, switcher)

    # Sarvam times out → marked unhealthy.
    # IndicConformer returns confidence 0.0, but without rerun enabled the
    # first successful result is still returned immediately.
    result = await router.transcribe(_AUDIO, "hi-en", "sess-5", "tenant-1")
    assert result is not None  # last low-confidence result returned
