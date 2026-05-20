"""Live integration tests — only run when RUN_LIVE_TESTS=1.

Cost guard: ≤20 chars of text per call.
Run: RUN_LIVE_TESTS=1 uv run pytest tests/integration -v
"""
from __future__ import annotations

import os

import pytest

pytestmark = pytest.mark.skipif(
    os.getenv("RUN_LIVE_TESTS") != "1",
    reason="set RUN_LIVE_TESTS=1 to run live provider tests",
)

_SHORT_TEXT = "Namaste"  # 7 chars — well within 20-char cost guard


# ── Sarvam Bulbul TTS ─────────────────────────────────────────────────────────

async def test_sarvam_bulbul_synthesize_live() -> None:
    from tts_router.engines.sarvam import SarvamBulbulEngine
    engine = SarvamBulbulEngine()
    assert engine._api_key, "SARVAM_API_KEY not set"
    audio = await engine.synthesize(_SHORT_TEXT, "anushka", "hi-en")
    assert isinstance(audio, bytes)
    assert len(audio) > 1024, f"Expected >1KB audio, got {len(audio)} bytes"
    print(f"[sarvam_bulbul] audio={len(audio)} bytes")


async def test_sarvam_bulbul_health_live() -> None:
    from tts_router.engines.sarvam import SarvamBulbulEngine
    engine = SarvamBulbulEngine()
    ok = await engine.health_check()
    assert ok, "Sarvam health check failed — check SARVAM_API_KEY"


# ── ElevenLabs TTS ────────────────────────────────────────────────────────────

async def test_elevenlabs_synthesize_live() -> None:
    from tts_router.engines.kokoro import ElevenLabsEngine
    engine = ElevenLabsEngine()
    assert engine._api_key, "ELEVENLABS_API_KEY not set"
    voice_id = os.getenv("ELEVENLABS_VOICE_ID", "zmh5xhBvMzqR4ZlXgcgL")
    audio = await engine.synthesize("Hello world", voice_id, "en")
    assert isinstance(audio, bytes)
    assert len(audio) > 1024, f"Expected >1KB audio, got {len(audio)} bytes"
    print(f"[elevenlabs] audio={len(audio)} bytes")


async def test_elevenlabs_health_live() -> None:
    from tts_router.engines.kokoro import ElevenLabsEngine
    engine = ElevenLabsEngine()
    ok = await engine.health_check()
    assert ok, "ElevenLabs health check failed — check ELEVENLABS_API_KEY"
