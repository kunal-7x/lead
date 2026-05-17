from __future__ import annotations

import struct

TARGET_RATE = 8000
BYTES_PER_SAMPLE = 2  # L16


def ensure_8khz_l16(audio: bytes, src_rate: int = 8000) -> bytes:
    """Ensure audio is L16 PCM 8kHz. Resample if src_rate != 8000."""
    if src_rate == TARGET_RATE:
        return audio
    # Simple nearest-neighbour decimation / interpolation
    samples = struct.unpack(f"<{len(audio)//2}h", audio)
    ratio = src_rate / TARGET_RATE
    out_len = int(len(samples) / ratio)
    out = [samples[min(int(i * ratio), len(samples) - 1)] for i in range(out_len)]
    return struct.pack(f"<{len(out)}h", *out)


def make_silent_pcm(duration_ms: int = 20, sample_rate: int = 8000) -> bytes:
    """Generate silent L16 PCM of the given duration."""
    n_samples = sample_rate * duration_ms // 1000
    return b"\x00\x00" * n_samples


def is_l16_pcm(audio: bytes) -> bool:
    """Heuristic: L16 PCM should be even-length bytes with no RIFF header."""
    return len(audio) % 2 == 0 and not audio.startswith(b"RIFF")


def strip_wav_header(wav: bytes) -> bytes:
    """Strip 44-byte WAV header if present, returning raw L16 PCM."""
    if wav[:4] == b"RIFF" and len(wav) > 44:
        return wav[44:]
    return wav
