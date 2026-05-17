from __future__ import annotations

import httpx
import pytest

from stt_router.engines.sarvam import SarvamEngine, _upsample_8k_to_16k, _wrap_pcm_wav, _lang_code
from stt_router.engines.indicconformer import IndicConformerEngine
from stt_router.engines.faster_whisper import FasterWhisperEngine
from stt_router.engines.groq_whisper import GroqWhisperEngine

_PCM = b"\x10\x20" * 160  # 20ms at 8kHz


# ── Sarvam utility functions ──────────────────────────────────────────────────

def test_lang_code_hinglish():
    assert _lang_code("hi-en") == "hi-IN"


def test_lang_code_hindi():
    assert _lang_code("hi") == "hi-IN"


def test_lang_code_english():
    assert _lang_code("en") == "en-IN"


def test_lang_code_unknown():
    assert _lang_code("fr") == "hi-IN"


def test_upsample_doubles_bytes():
    pcm = b"\x00\x01" * 50
    out = _upsample_8k_to_16k(pcm)
    assert len(out) == len(pcm) * 2


def test_upsample_empty():
    out = _upsample_8k_to_16k(b"")
    assert out == b""


def test_wrap_pcm_wav_riff():
    wav = _wrap_pcm_wav(b"\x00" * 320, 16000)
    assert wav[:4] == b"RIFF"
    assert wav[8:12] == b"WAVE"
    assert wav[12:16] == b"fmt "


def test_wrap_pcm_wav_size():
    pcm = b"\x00" * 100
    wav = _wrap_pcm_wav(pcm, 8000)
    # WAV header = 44 bytes
    assert len(wav) == 44 + len(pcm)


# ── Sarvam engine (mocked HTTP) ───────────────────────────────────────────────

def _sarvam_mock_transport():
    return httpx.MockTransport(
        handler=lambda request: httpx.Response(
            200, json={"transcript": "mocked sarvam", "confidence": 0.91}
        )
    )


async def test_sarvam_engine_transcribe_mocked():
    engine = SarvamEngine(api_key="test-key")
    engine._client = httpx.AsyncClient(transport=_sarvam_mock_transport())
    result = await engine.transcribe(_PCM, "hi-en", "sess-1")
    assert result.text == "mocked sarvam"
    assert result.engine_used == "sarvam"
    assert result.confidence == 0.91


async def test_sarvam_engine_no_key_unhealthy():
    engine = SarvamEngine(api_key="")
    healthy = await engine.health_check()
    assert healthy is False


# ── IndicConformer engine (mocked HTTP) ───────────────────────────────────────

def _indic_mock_transport():
    return httpx.MockTransport(
        handler=lambda request: httpx.Response(
            200, json={"text": "mocked indic", "confidence": 0.85}
        )
    )


async def test_indicconformer_engine_transcribe_mocked():
    engine = IndicConformerEngine()
    engine._client = httpx.AsyncClient(transport=_indic_mock_transport())
    result = await engine.transcribe(_PCM, "hi", "sess-2")
    assert result.text == "mocked indic"
    assert result.engine_used == "indicconformer"


async def test_indicconformer_health_unreachable():
    engine = IndicConformerEngine(base_url="http://127.0.0.1:19999")
    healthy = await engine.health_check()
    assert healthy is False


# ── FasterWhisper engine (mocked HTTP) ───────────────────────────────────────

def _whisper_mock_transport():
    return httpx.MockTransport(
        handler=lambda request: httpx.Response(
            200, json={"text": "mocked whisper", "confidence": 0.80}
        )
    )


async def test_faster_whisper_engine_transcribe_mocked():
    engine = FasterWhisperEngine()
    engine._client = httpx.AsyncClient(transport=_whisper_mock_transport())
    result = await engine.transcribe(_PCM, "en", "sess-3")
    assert result.text == "mocked whisper"
    assert result.engine_used == "faster_whisper"


async def test_faster_whisper_health_unreachable():
    engine = FasterWhisperEngine(base_url="http://127.0.0.1:19999")
    healthy = await engine.health_check()
    assert healthy is False


# ── GroqWhisper engine (mocked HTTP) ─────────────────────────────────────────

def _groq_mock_transport():
    return httpx.MockTransport(
        handler=lambda request: httpx.Response(
            200, json={"text": "mocked groq"}
        )
    )


async def test_groq_whisper_engine_transcribe_mocked():
    engine = GroqWhisperEngine(api_key="test-key")
    engine._client = httpx.AsyncClient(transport=_groq_mock_transport())
    result = await engine.transcribe(_PCM, "hi-en", "sess-4")
    assert result.text == "mocked groq"
    assert result.engine_used == "groq_whisper"


async def test_groq_whisper_no_key_unhealthy():
    engine = GroqWhisperEngine(api_key="")
    healthy = await engine.health_check()
    assert healthy is False
