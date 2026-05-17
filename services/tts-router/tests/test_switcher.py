from __future__ import annotations

import pytest
import fakeredis.aioredis

from tts_router.switcher import EngineSwitcher, DEFAULT_ENGINE, _GLOBAL_KEY
from tts_router.router import TTSRouter
from tts_router.cache import FakeAudioCache
from tests.fakes.fake_engines import FakeSarvamTTS, FakeKokoroTTS
from tests.conftest import make_req


async def test_default_engine(switcher):
    active = await switcher.active_engine("t1")
    assert active == DEFAULT_ENGINE == "sarvam_bulbul"


async def test_engine_switcher(fake_redis):
    """Redis tts:global_engine=kokoro → routes to Kokoro."""
    await fake_redis.set(_GLOBAL_KEY, "kokoro")
    switcher = EngineSwitcher(fake_redis)

    kokoro = FakeKokoroTTS()
    sarvam = FakeSarvamTTS()
    router = TTSRouter(
        {"sarvam_bulbul": sarvam, "kokoro": kokoro},
        switcher,
        FakeAudioCache(),
    )

    result = await router.synthesize(make_req())
    assert result.tier_used == "kokoro"
    assert kokoro.call_count == 1
    assert sarvam.call_count == 0


async def test_per_tenant_override(fake_redis):
    """Tenant A: global (sarvam). Tenant B: kokoro override."""
    await fake_redis.set("tts:tenant:tenant-b:engine", "kokoro")
    switcher = EngineSwitcher(fake_redis)

    kokoro = FakeKokoroTTS()
    sarvam = FakeSarvamTTS()
    router = TTSRouter({"sarvam_bulbul": sarvam, "kokoro": kokoro}, switcher, FakeAudioCache())

    r_a = await router.synthesize(make_req(tenant_id="tenant-a"))
    assert r_a.tier_used == "sarvam_bulbul"

    r_b = await router.synthesize(make_req(tenant_id="tenant-b", text="different text"))
    assert r_b.tier_used == "kokoro"


async def test_invalid_redis_falls_back_to_default(fake_redis):
    await fake_redis.set(_GLOBAL_KEY, "bad_engine")
    switcher = EngineSwitcher(fake_redis)
    active = await switcher.active_engine("t1")
    assert active == DEFAULT_ENGINE


async def test_set_invalid_engine_raises(switcher):
    with pytest.raises(ValueError, match="Invalid engine"):
        await switcher.set_global_engine("nonexistent")
