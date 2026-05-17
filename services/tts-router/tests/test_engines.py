from __future__ import annotations

import httpx
import pytest

from tts_router.engines.sarvam import SarvamBulbulEngine, _lang_code
from tts_router.engines.kokoro import KokoroEngine, IndicParlerEngine, IndicF5Engine, ElevenLabsEngine
from tts_router.audio import strip_wav_header
import struct, base64


def _make_pcm(n: int = 320) -> bytes:
    return struct.pack(f"<{n//2}h", *([0] * (n // 2)))


def _make_wav(n_samples: int = 160) -> bytes:
    pcm = _make_pcm(n_samples * 2)
    header = b"RIFF" + b"\x00" * 8 + b"WAVE" + b"\x00" * 28
    return header + pcm


def _mock(body, status=200):
    return httpx.MockTransport(handler=lambda r: httpx.Response(status, json=body))


def _mock_bytes(content: bytes, status=200):
    return httpx.MockTransport(handler=lambda r: httpx.Response(status, content=content))


# ── Sarvam ────────────────────────────────────────────────────────────────────

async def test_sarvam_synthesize_mocked():
    engine = SarvamBulbulEngine(api_key="test-key")
    wav_b64 = base64.b64encode(_make_wav()).decode()
    engine._client = httpx.AsyncClient(transport=_mock({"audios": [wav_b64]}))
    result = await engine.synthesize("hello", "meera", "hi-en")
    assert len(result) > 0


async def test_sarvam_health_no_key():
    assert await SarvamBulbulEngine(api_key="").health_check() is False


async def test_sarvam_health_with_key():
    assert await SarvamBulbulEngine(api_key="key").health_check() is True


def test_sarvam_voices():
    voices = SarvamBulbulEngine(api_key="x").voices()
    assert any(v.id == "meera" for v in voices)


def test_lang_code_mapping():
    assert _lang_code("hi-en") == "hi-IN"
    assert _lang_code("en") == "en-IN"
    assert _lang_code("unknown") == "hi-IN"


# ── Kokoro ────────────────────────────────────────────────────────────────────

async def test_kokoro_synthesize_mocked():
    engine = KokoroEngine()
    engine._client = httpx.AsyncClient(transport=_mock_bytes(_make_pcm()))
    result = await engine.synthesize("hello", "af_heart", "hi-en")
    assert len(result) > 0


async def test_kokoro_health_unreachable():
    engine = KokoroEngine(base_url="http://127.0.0.1:19999")
    assert await engine.health_check() is False


async def test_kokoro_health_mocked():
    engine = KokoroEngine()
    engine._client = httpx.AsyncClient(transport=_mock({}, 200))
    assert await engine.health_check() is True


def test_kokoro_voices():
    assert len(KokoroEngine().voices()) > 0


# ── IndicParler ───────────────────────────────────────────────────────────────

async def test_indic_parler_synthesize_mocked():
    engine = IndicParlerEngine()
    engine._client = httpx.AsyncClient(transport=_mock_bytes(_make_pcm()))
    result = await engine.synthesize("hello", "default", "hi")
    assert len(result) > 0


async def test_indic_parler_health_unreachable():
    engine = IndicParlerEngine(base_url="http://127.0.0.1:19999")
    assert await engine.health_check() is False


# ── IndicF5 ───────────────────────────────────────────────────────────────────

async def test_indicf5_synthesize_mocked():
    engine = IndicF5Engine()
    engine._client = httpx.AsyncClient(transport=_mock_bytes(_make_pcm()))
    result = await engine.synthesize("hello", "custom", "hi")
    assert len(result) > 0


async def test_indicf5_health_unreachable():
    engine = IndicF5Engine(base_url="http://127.0.0.1:19999")
    assert await engine.health_check() is False


# ── ElevenLabs ────────────────────────────────────────────────────────────────

async def test_elevenlabs_synthesize_mocked():
    engine = ElevenLabsEngine(api_key="test-key")
    engine._client = httpx.AsyncClient(transport=_mock_bytes(_make_pcm()))
    result = await engine.synthesize("hello", "21m00Tcm4TlvDq8ikWAM", "en")
    assert len(result) > 0


async def test_elevenlabs_health_no_key():
    assert await ElevenLabsEngine(api_key="").health_check() is False


async def test_elevenlabs_health_with_key():
    assert await ElevenLabsEngine(api_key="key").health_check() is True


def test_elevenlabs_premium_only():
    assert ElevenLabsEngine().premium_only is True


# ── App ───────────────────────────────────────────────────────────────────────

def test_app_healthz(monkeypatch):
    from fastapi.testclient import TestClient
    from tts_router.app import app
    import tts_router.app as app_module
    from tts_router.router import TTSRouter
    from tts_router.cache import FakeAudioCache
    from tts_router.switcher import EngineSwitcher
    import fakeredis.aioredis
    from tests.fakes.fake_engines import FakeSarvamTTS

    def _fake_router():
        rdb = fakeredis.aioredis.FakeRedis()
        return TTSRouter(
            {"sarvam_bulbul": FakeSarvamTTS()},
            EngineSwitcher(rdb),
            FakeAudioCache(),
        )

    monkeypatch.setattr(app_module, "_build_router", _fake_router)
    with TestClient(app) as c:
        resp = c.get("/healthz")
    assert resp.status_code == 200


def test_app_voices(monkeypatch):
    from fastapi.testclient import TestClient
    from tts_router.app import app
    import tts_router.app as app_module
    from tts_router.router import TTSRouter
    from tts_router.cache import FakeAudioCache
    from tts_router.switcher import EngineSwitcher
    import fakeredis.aioredis
    from tests.fakes.fake_engines import FakeSarvamTTS

    def _fake_router():
        rdb = fakeredis.aioredis.FakeRedis()
        return TTSRouter(
            {"sarvam_bulbul": FakeSarvamTTS()},
            EngineSwitcher(rdb),
            FakeAudioCache(),
        )

    monkeypatch.setattr(app_module, "_build_router", _fake_router)
    with TestClient(app) as c:
        resp = c.get("/v1/tts/voices")
    assert resp.status_code == 200
