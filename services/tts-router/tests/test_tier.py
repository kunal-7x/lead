from __future__ import annotations

from tts_router.router import TTSRouter
from tts_router.cache import FakeAudioCache
from tests.fakes.fake_engines import FakeSarvamTTS, FakeElevenLabsTTS
from tests.conftest import make_req


async def test_elevenlabs_gated(switcher):
    """ElevenLabs blocked for non-premium tenants."""
    sarvam = FakeSarvamTTS(timeout=True)  # fail sarvam → should NOT fall to ElevenLabs
    el = FakeElevenLabsTTS()
    router = TTSRouter(
        {"sarvam_bulbul": sarvam, "elevenlabs": el},
        switcher,
        FakeAudioCache(),
    )

    # Non-premium request → ElevenLabs must NOT be called
    result = await router.synthesize(make_req(tts_premium=False))
    assert el.call_count == 0
    assert result.tier_used == "silence"  # sarvam timed out, elevenlabs gated


async def test_elevenlabs_allowed_for_premium(switcher):
    """ElevenLabs IS called when tts_premium=True and sarvam fails."""
    sarvam = FakeSarvamTTS(timeout=True)
    el = FakeElevenLabsTTS()
    router = TTSRouter(
        {"sarvam_bulbul": sarvam, "elevenlabs": el},
        switcher,
        FakeAudioCache(),
    )

    result = await router.synthesize(make_req(tts_premium=True))
    assert el.call_count == 1
    assert result.tier_used == "elevenlabs"


async def test_sarvam_non_premium_still_works(switcher):
    """sarvam_bulbul (non-premium) works for all tenants."""
    sarvam = FakeSarvamTTS()
    router = TTSRouter({"sarvam_bulbul": sarvam}, switcher, FakeAudioCache())
    result = await router.synthesize(make_req(tts_premium=False))
    assert result.tier_used == "sarvam_bulbul"
