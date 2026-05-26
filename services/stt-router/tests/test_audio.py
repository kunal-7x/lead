from __future__ import annotations

import struct

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


def test_upsample_numpy_values_match_reference():
    """Numpy vectorized upsample produces same values as the reference pure-Python algorithm."""
    import numpy as np

    # Build a reference implementation (the original pure-Python loop)
    def _ref_upsample(pcm8k: bytes) -> bytes:
        samples = struct.unpack(f"<{len(pcm8k) // 2}h", pcm8k)
        out: list[int] = []
        for i, s in enumerate(samples):
            out.append(s)
            nxt = samples[i + 1] if i + 1 < len(samples) else s
            out.append((s + nxt) // 2)
        return struct.pack(f"<{len(out)}h", *out)

    # Test with a variety of values including edge cases
    rng = np.random.default_rng(42)
    raw_samples = rng.integers(-32768, 32767, size=200, dtype=np.int16)
    pcm = raw_samples.tobytes()

    ref = _ref_upsample(pcm)
    got = _upsample_8k_to_16k(pcm)

    assert len(got) == len(ref), "output length mismatch"
    ref_arr = np.frombuffer(ref, dtype="<i2")
    got_arr = np.frombuffer(got, dtype="<i2")
    assert np.array_equal(ref_arr, got_arr), "sample values differ from reference"


def test_upsample_empty():
    """Empty input returns empty output without error."""
    assert _upsample_8k_to_16k(b"") == b""


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
