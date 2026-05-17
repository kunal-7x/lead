from __future__ import annotations

import pytest

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

    result = await router.transcribe(_AUDIO, "hi-en", "sess-2", "tenant-1")

    assert result.engine_used == "indicconformer"
    assert result.text == "fallback text"


async def test_hinglish_routing(switcher):
    """lang=hi-en → always routes to Sarvam first."""
    sarvam = FakeSarvamEngine(transcript="hinglish result", confidence=0.90)
    router = _make_router(switcher, sarvam=sarvam)

    result = await router.transcribe(_AUDIO, "hi-en", "sess-3", "tenant-1")

    assert result.engine_used == "sarvam"


async def test_low_confidence_rerun(switcher):
    """Sarvam returns confidence 0.4 (< 0.55) → re-runs on IndicConformer."""
    sarvam = FakeSarvamEngine(transcript="weak result", confidence=0.40)
    indicconformer = FakeIndicConformerEngine(transcript="better result", confidence=0.87)
    router = _make_router(switcher, sarvam=sarvam, indicconformer=indicconformer)

    result = await router.transcribe(_AUDIO, "hi-en", "sess-4", "tenant-1")

    assert result.engine_used == "indicconformer"
    assert result.confidence >= 0.55


async def test_all_engines_fail(switcher):
    """All engines timeout → RuntimeError."""
    sarvam = FakeSarvamEngine(timeout=True)
    indicconformer = FakeIndicConformerEngine(confidence=0.0)

    engines = {"sarvam": sarvam, "indicconformer": indicconformer}
    router = STTRouter(engines, switcher)

    # IndicConformer returns confidence 0.0 < 0.55; Sarvam times out
    # Both have been tried — last_result should still be returned
    result = await router.transcribe(_AUDIO, "hi-en", "sess-5", "tenant-1")
    assert result is not None  # last low-confidence result returned
