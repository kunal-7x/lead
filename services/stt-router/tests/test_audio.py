from __future__ import annotations

import pytest

from stt_router.engines.sarvam import _upsample_8k_to_16k, _wrap_pcm_wav
from stt_router.router import STTRouter
from stt_router.switcher import EngineSwitcher
from tests.fakes.fake_sarvam import FakeSarvamEngine


async def test_8khz_input_accepted(switcher):
    """8kHz L16 PCM from FreeSWITCH is accepted and transcribed without error."""
    # 20ms at 8kHz = 160 samples = 320 bytes
    pcm_8khz = b"\x10\x20" * 160
    sarvam = FakeSarvamEngine(transcript="test ok", confidence=0.90)
    router = STTRouter({"sarvam": sarvam}, switcher)

    result = await router.transcribe(pcm_8khz, "hi-en", "sess-audio", "tenant-1")
    assert result.text == "test ok"
    assert result.engine_used == "sarvam"


def test_upsample_8k_to_16k_doubles_length():
    """Upsampler doubles the number of samples (8kHz → 16kHz)."""
    pcm = b"\x00\x01" * 100  # 100 samples
    out = _upsample_8k_to_16k(pcm)
    assert len(out) == len(pcm) * 2


def test_wrap_pcm_wav_has_riff_header():
    """WAV wrapper produces a valid RIFF header."""
    pcm = b"\x00" * 320
    wav = _wrap_pcm_wav(pcm, sample_rate=16000)
    assert wav[:4] == b"RIFF"
    assert wav[8:12] == b"WAVE"


def test_wrap_pcm_wav_8khz():
    pcm = b"\x00" * 320
    wav = _wrap_pcm_wav(pcm, sample_rate=8000)
    assert wav[:4] == b"RIFF"
