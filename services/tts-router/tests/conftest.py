from __future__ import annotations

import pytest
import fakeredis.aioredis

from tts_router.cache import FakeAudioCache
from tts_router.router import TTSRouter
from tts_router.switcher import EngineSwitcher
from tts_router.models import TTSRequest
from tests.fakes.fake_engines import FakeSarvamTTS, FakeKokoroTTS, FakeElevenLabsTTS


@pytest.fixture
async def fake_redis():
    return fakeredis.aioredis.FakeRedis()


@pytest.fixture
async def switcher(fake_redis):
    return EngineSwitcher(fake_redis)


@pytest.fixture
def fake_cache():
    return FakeAudioCache()


@pytest.fixture
def engines():
    return {
        "sarvam_bulbul": FakeSarvamTTS(),
        "kokoro": FakeKokoroTTS(),
        "elevenlabs": FakeElevenLabsTTS(),
    }


@pytest.fixture
def router(engines, switcher, fake_cache):
    return TTSRouter(engines, switcher, fake_cache)


def make_req(**kwargs) -> TTSRequest:
    defaults = {
        "text": "Namaste, aapka swagat hai.",
        "lang": "hi-en",
        "voice_id": "meera",
        "tenant_id": "tenant-1",
        "session_id": "sess-1",
    }
    defaults.update(kwargs)
    return TTSRequest(**defaults)
