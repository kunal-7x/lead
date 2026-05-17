from __future__ import annotations

import time
import pytest

from tts_router.cache import FakeAudioCache, cache_key
from tests.conftest import make_req


async def test_cache_hit_latency(router, fake_cache, engines):
    """Same text twice → second call hits cache (sub-50ms, tier=cache)."""
    req = make_req(text="Aapka budget kya hai?")

    # First call — cache miss, synthesizes
    r1 = await router.synthesize(req)
    assert r1.cache_hit is False
    assert r1.tier_used == "sarvam_bulbul"

    # Second call — cache hit
    t0 = time.time()
    r2 = await router.synthesize(req)
    elapsed_ms = (time.time() - t0) * 1000

    assert r2.cache_hit is True
    assert r2.tier_used == "cache"
    assert elapsed_ms < 50, f"Cache hit took {elapsed_ms:.1f}ms, expected <50ms"
    assert r1.audio == r2.audio


async def test_different_texts_not_cached(router):
    """Different texts produce different cache entries."""
    r1 = await router.synthesize(make_req(text="Text one"))
    r2 = await router.synthesize(make_req(text="Text two"))
    assert r1.cache_hit is False
    assert r2.cache_hit is False


async def test_cache_key_includes_voice(fake_cache):
    """Different voice IDs produce different cache keys."""
    k1 = cache_key("hello", "meera", "hi-en")
    k2 = cache_key("hello", "arvind", "hi-en")
    assert k1 != k2


async def test_cache_key_includes_lang(fake_cache):
    k1 = cache_key("hello", "meera", "hi-en")
    k2 = cache_key("hello", "meera", "en")
    assert k1 != k2


async def test_cache_miss_count(router, fake_cache):
    await router.synthesize(make_req(text="miss this"))
    assert fake_cache.miss_count == 1
    assert fake_cache.hit_count == 0
