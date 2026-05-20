"""Live integration tests — only run when RUN_LIVE_TESTS=1.

Cost guard: sends ≤5 seconds of audio per test.
Run: RUN_LIVE_TESTS=1 uv run pytest tests/integration -v
"""
from __future__ import annotations

import os
import pathlib

import pytest

pytestmark = pytest.mark.skipif(
    os.getenv("RUN_LIVE_TESTS") != "1",
    reason="set RUN_LIVE_TESTS=1 to run live provider tests",
)

_FIXTURE = pathlib.Path(__file__).parent.parent / "fixtures" / "sample_hindi.wav"


@pytest.fixture
def sample_audio() -> bytes:
    # Strip 44-byte WAV header → raw 8kHz L16 PCM as engines expect
    return _FIXTURE.read_bytes()[44:]


# ── Sarvam STT ────────────────────────────────────────────────────────────────

async def test_sarvam_transcribe_live(sample_audio: bytes) -> None:
    from stt_router.engines.sarvam import SarvamEngine
    engine = SarvamEngine()
    assert engine._api_key, "SARVAM_API_KEY not set"
    result = await engine.transcribe(sample_audio, "hi-en", "live-test")
    assert isinstance(result.text, str)
    assert result.engine_used == "sarvam"
    assert result.latency_ms > 0
    print(f"[sarvam] text={result.text.encode('ascii', 'replace').decode()!r} latency={result.latency_ms}ms")


async def test_sarvam_health_live() -> None:
    from stt_router.engines.sarvam import SarvamEngine
    engine = SarvamEngine()
    ok = await engine.health_check()
    assert ok, "Sarvam health check failed — check SARVAM_API_KEY"


# ── Groq Whisper STT ──────────────────────────────────────────────────────────

async def test_groq_whisper_transcribe_live(sample_audio: bytes) -> None:
    from stt_router.engines.groq_whisper import GroqWhisperEngine
    engine = GroqWhisperEngine()
    assert engine._api_key, "GROQ_API_KEY not set"
    result = await engine.transcribe(sample_audio, "hi-en", "live-test")
    assert isinstance(result.text, str)
    assert result.engine_used == "groq_whisper"
    assert result.latency_ms > 0
    print(f"[groq_whisper] text={result.text.encode('ascii', 'replace').decode()!r} latency={result.latency_ms}ms")


async def test_groq_whisper_health_live() -> None:
    from stt_router.engines.groq_whisper import GroqWhisperEngine
    engine = GroqWhisperEngine()
    ok = await engine.health_check()
    assert ok, "Groq health check failed — check GROQ_API_KEY"
