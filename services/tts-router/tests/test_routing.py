from __future__ import annotations

from tts_router.router import TTSRouter
from tts_router.cache import FakeAudioCache
from tests.fakes.fake_engines import FakeSarvamTTS, FakeKokoroTTS
from tests.conftest import make_req


async def test_sarvam_primary(switcher):
    """Default engine is sarvam_bulbul → tier=sarvam_bulbul."""
    sarvam = FakeSarvamTTS()
    kokoro = FakeKokoroTTS()
    router = TTSRouter({"sarvam_bulbul": sarvam, "kokoro": kokoro}, switcher, FakeAudioCache())

    result = await router.synthesize(make_req())

    assert result.tier_used == "sarvam_bulbul"
    assert sarvam.call_count == 1
    assert kokoro.call_count == 0


async def test_sarvam_timeout_fallback(switcher):
    """Sarvam times out → falls back to kokoro."""
    sarvam = FakeSarvamTTS(timeout=True)
    kokoro = FakeKokoroTTS()
    router = TTSRouter({"sarvam_bulbul": sarvam, "kokoro": kokoro}, switcher, FakeAudioCache())

    result = await router.synthesize(make_req())

    assert result.tier_used == "kokoro"
    assert kokoro.call_count == 1


async def test_all_engines_fail_returns_silence(switcher):
    """All engines fail → silent PCM returned."""
    from tests.fakes.fake_engines import FakeSarvamTTS
    bad = FakeSarvamTTS(timeout=True)
    router = TTSRouter({"sarvam_bulbul": bad}, switcher, FakeAudioCache())

    result = await router.synthesize(make_req())
    assert result.tier_used == "silence"
    assert len(result.audio) > 0


async def test_output_audio_non_empty(switcher):
    """Synthesized audio is non-empty bytes."""
    sarvam = FakeSarvamTTS()
    router = TTSRouter({"sarvam_bulbul": sarvam}, switcher, FakeAudioCache())
    result = await router.synthesize(make_req())
    assert len(result.audio) > 0
