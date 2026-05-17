from __future__ import annotations

import struct

from tts_router.audio import ensure_8khz_l16, strip_wav_header, make_silent_pcm, is_l16_pcm
from tests.conftest import make_req


def test_output_is_8khz_l16(router):
    """ensure_8khz_l16 is idempotent when src is already 8kHz."""
    pcm = b"\x10\x20" * 160
    out = ensure_8khz_l16(pcm, src_rate=8000)
    assert out == pcm


def test_resample_16k_to_8k():
    """16kHz input is downsampled to 8kHz (half the samples)."""
    pcm_16k = b"\x00\x01" * 1600  # 1600 samples at 16kHz = 100ms
    out = ensure_8khz_l16(pcm_16k, src_rate=16000)
    # Should be roughly 800 samples
    assert len(out) == 800 * 2


def test_strip_wav_header_removes_44_bytes():
    header = b"RIFF" + b"\x00" * 8 + b"WAVE" + b"\x00" * 28  # 44 bytes
    payload = b"\x10\x20" * 100
    wav = header + payload
    assert strip_wav_header(wav) == payload


def test_strip_wav_header_no_header():
    raw = b"\x10\x20" * 100
    assert strip_wav_header(raw) == raw


def test_make_silent_pcm_length():
    pcm = make_silent_pcm(20, 8000)
    # 20ms at 8kHz = 160 samples = 320 bytes
    assert len(pcm) == 320
    assert all(b == 0 for b in pcm)


def test_is_l16_pcm_true():
    assert is_l16_pcm(b"\x00" * 320) is True


def test_is_l16_pcm_wav_false():
    assert is_l16_pcm(b"RIFF" + b"\x00" * 40) is False


async def test_router_output_is_even_bytes(router):
    """Router output is always even-length (valid L16 PCM)."""
    result = await router.synthesize(make_req())
    assert len(result.audio) % 2 == 0


async def test_router_output_sample_rate(router):
    """Router result reports 8000 Hz sample rate."""
    result = await router.synthesize(make_req())
    assert result.sample_rate == 8000
