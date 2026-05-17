from __future__ import annotations

import pytest

from stt_router.router import STTRouter
from stt_router.switcher import EngineSwitcher, DEFAULT_ENGINE, REDIS_KEY
from tests.fakes.fake_sarvam import FakeSarvamEngine
from tests.fakes.fake_indicconformer import FakeIndicConformerEngine

_AUDIO = b"\x00\x01" * 320


async def test_engine_switcher_default(fake_redis):
    """No Redis key set → default engine is sarvam."""
    switcher = EngineSwitcher(fake_redis)
    active = await switcher.active_engine()
    assert active == DEFAULT_ENGINE


async def test_engine_switcher_redis(fake_redis):
    """Setting Redis key stt:active_engine=indicconformer → routes there."""
    await fake_redis.set(REDIS_KEY, "indicconformer")
    switcher = EngineSwitcher(fake_redis)

    indicconformer = FakeIndicConformerEngine(transcript="indic result", confidence=0.88)
    sarvam = FakeSarvamEngine(transcript="sarvam result", confidence=0.92)

    router = STTRouter(
        {"sarvam": sarvam, "indicconformer": indicconformer},
        switcher,
    )

    result = await router.transcribe(_AUDIO, "hi-en", "sess-sw", "tenant-1")
    assert result.engine_used == "indicconformer"
    assert result.text == "indic result"


async def test_engine_switcher_invalid_value(fake_redis):
    """Invalid Redis value → falls back to default engine."""
    await fake_redis.set(REDIS_KEY, "nonexistent_engine")
    switcher = EngineSwitcher(fake_redis)
    active = await switcher.active_engine()
    assert active == DEFAULT_ENGINE


async def test_set_engine(fake_redis):
    """set_engine() writes the Redis key."""
    switcher = EngineSwitcher(fake_redis)
    await switcher.set_engine("groq_whisper")
    active = await switcher.active_engine()
    assert active == "groq_whisper"


async def test_set_engine_invalid(fake_redis):
    """set_engine() raises ValueError for invalid engine names."""
    switcher = EngineSwitcher(fake_redis)
    with pytest.raises(ValueError, match="Invalid engine"):
        await switcher.set_engine("bogus")
