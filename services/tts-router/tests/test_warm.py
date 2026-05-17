from __future__ import annotations

from tts_router.router import TTSRouter
from tts_router.cache import FakeAudioCache
from tests.fakes.fake_engines import FakeSarvamTTS
from tests.conftest import make_req


async def test_warm_populates_cache(switcher):
    """Warm with 20 static phrases → 20 cache entries."""
    sarvam = FakeSarvamTTS()
    cache = FakeAudioCache()
    router = TTSRouter({"sarvam_bulbul": sarvam}, switcher, cache)

    phrases = [f"Phrase number {i}" for i in range(20)]
    count = await router.warm(phrases, lang="hi-en", voice_id="meera", tenant_id="t1")

    assert count == 20
    assert cache.size() == 20
    assert sarvam.call_count == 20


async def test_warm_subsequent_hits_cache(switcher):
    """After warming, synthesizing same text hits cache."""
    sarvam = FakeSarvamTTS()
    cache = FakeAudioCache()
    router = TTSRouter({"sarvam_bulbul": sarvam}, switcher, cache)

    await router.warm(["Aapka budget kya hai?"], lang="hi-en", voice_id="meera", tenant_id="t1")
    initial_calls = sarvam.call_count

    result = await router.synthesize(make_req(text="Aapka budget kya hai?"))
    assert result.cache_hit is True
    assert sarvam.call_count == initial_calls  # no new synthesis call


async def test_warm_zero_phrases(switcher):
    sarvam = FakeSarvamTTS()
    cache = FakeAudioCache()
    router = TTSRouter({"sarvam_bulbul": sarvam}, switcher, cache)
    count = await router.warm([], lang="hi-en", voice_id="meera", tenant_id="t1")
    assert count == 0
    assert cache.size() == 0
