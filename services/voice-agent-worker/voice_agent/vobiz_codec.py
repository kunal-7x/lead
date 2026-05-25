"""vobiz_codec.py — Pure-Python (NumPy) G.711 µ-law ↔ PCM-16 codec.

No dependency on stdlib audioop (removed in Python 3.13).
Both sides of the Vobiz bridge operate at 8 kHz, so NO sample-rate conversion
is needed to feed AgentLoop, which already receives 8 kHz L16 PCM from /ws/audio.

Optional helpers resample_8k_to_16k / resample_16k_to_8k are provided for
future STT backends that require 16 kHz input; they use NumPy linear interpolation.
AgentLoop is NOT changed — it remains on 8 kHz throughout.

Algorithm
---------
Decode (ulaw → PCM16):
    u_comp = ~ulaw_byte
    sign   = u_comp >> 7          (1 = negative)
    mag7   = u_comp & 0x7F        (7-bit magnitude field)
    exp    = (mag7 >> 4) & 7      (3-bit segment exponent)
    mant   = mag7 & 0xF           (4-bit mantissa)
    lin14  = ((mant << 1) | 0x21) << exp  - 33   (14-bit linear, minus bias 33)
    lin16  = lin14 * 4                            (scale to 16-bit)
    result = -lin16 if sign else lin16

Encode (PCM16 → ulaw):
    t      = pcm16 >> 2           (arithmetic right-shift to 14-bit; Python >> = floor)
    sign   = 1 if t < 0 else 0
    t      = abs(t), clamped to 8158 (biased max = 8191 = 2^13 - 1)
    biased = t + 33
    exp    = max(0, biased.bit_length() - 6)   (i.e. highest-bit-position - 5)
    mant   = (biased >> (exp + 1)) & 0xF
    u_comp = (sign << 7) | (exp << 4) | mant
    ulaw   = ~u_comp & 0xFF

These formulas match CPython's audioop.lin2ulaw/ulaw2lin for all 65536 values
(verified on Python 3.12). audioop is not imported at runtime.
"""
from __future__ import annotations

import numpy as np


# ---------------------------------------------------------------------------
# Build G.711 µ-law lookup tables using the verified formulas above.
# ---------------------------------------------------------------------------

def _build_decode_table() -> np.ndarray:
    """Build a 256-entry int16 table: µ-law byte index → PCM16 sample."""
    indices = np.arange(256, dtype=np.int32)
    u_comp = (~indices) & 0xFF                       # complement
    sign = (u_comp >> 7) & 1                         # 1 = negative
    mag7 = u_comp & 0x7F
    exp = (mag7 >> 4) & 7
    mant = mag7 & 0x0F
    lin14 = ((mant << 1) | 0x21) << exp              # 14-bit magnitude
    lin14 -= 33                                       # remove bias
    lin16 = lin14 * 4                                 # scale to 16-bit
    lin16 = np.where(sign, -lin16, lin16)
    # Clamp to int16 range (handles edge cases)
    return np.clip(lin16, -32768, 32767).astype(np.int16)


def _build_encode_table() -> np.ndarray:
    """Build a 65536-entry uint8 table: PCM16 (offset by +32768) → µ-law byte.

    Index mapping: table[sample + 32768] for sample in [-32768, 32767].
    Uses Python floor-division semantics for the >>2 step (matches C audioop).
    """
    # Build for all signed int16 values represented as int32
    samples = np.arange(65536, dtype=np.int32) - 32768   # [-32768 .. 32767]

    # Arithmetic right-shift by 2 (Python // 4 = floor = same as C arith >>2)
    t = samples >> 2   # numpy int32 >> preserves sign (arithmetic shift)

    sign = (t < 0).astype(np.uint8)                  # 1 if negative
    t_abs = np.abs(t)
    t_clipped = np.minimum(t_abs, 8158)              # clip before bias
    biased = t_clipped + 33                          # add µ-law bias

    # Exponent: highest bit position of biased, minus 5
    # biased range: 33..8191; bit_length range: 6..13; exp range: 0..7 (clamped ≥ 0)
    # Compute highest-bit-position via log2 floor
    # For biased in [33..8191]: log2 in [5.04..12.9], floor = [5..12]
    exp = np.floor(np.log2(biased.astype(np.float64))).astype(np.int32) - 5
    exp = np.clip(exp, 0, 7).astype(np.uint8)

    mant = ((biased >> (exp.astype(np.int32) + 1)) & 0x0F).astype(np.uint8)
    u_comp = ((sign << 7) | (exp << 4) | mant).astype(np.uint8)
    return np.bitwise_not(u_comp)


# Build tables once at module import (< 5 ms)
_DECODE_TABLE: np.ndarray = _build_decode_table()
_ENCODE_TABLE: np.ndarray = _build_encode_table()


# ---------------------------------------------------------------------------
# Public codec functions
# ---------------------------------------------------------------------------

def ulaw_to_pcm16(ulaw_bytes: bytes) -> bytes:
    """Decode G.711 µ-law bytes → signed 16-bit LE PCM bytes at 8 kHz.

    Args:
        ulaw_bytes: Raw µ-law encoded audio (1 byte per sample, 8 kHz).
    Returns:
        PCM16 bytes (2 bytes per sample, little-endian, 8 kHz).
        Output length = len(ulaw_bytes) * 2.
    """
    arr = np.frombuffer(ulaw_bytes, dtype=np.uint8)
    pcm = _DECODE_TABLE[arr]        # lookup: uint8 → int16
    return pcm.astype("<i2").tobytes()


def pcm16_to_ulaw(pcm_bytes: bytes) -> bytes:
    """Encode signed 16-bit LE PCM bytes at 8 kHz → G.711 µ-law bytes.

    Args:
        pcm_bytes: PCM16 audio (2 bytes per sample, little-endian, 8 kHz).
    Returns:
        µ-law bytes (1 byte per sample, 8 kHz).
        Output length = len(pcm_bytes) // 2.
    """
    samples = np.frombuffer(pcm_bytes, dtype="<i2").astype(np.int32)
    # Map signed int32 → 0..65535 index into encode table
    indices = np.clip(samples + 32768, 0, 65535).astype(np.int32)
    return _ENCODE_TABLE[indices].tobytes()


# ---------------------------------------------------------------------------
# Optional: sample-rate conversion helpers (not used by the Vobiz bridge)
# ---------------------------------------------------------------------------

def resample_8k_to_16k(pcm8k: bytes) -> bytes:
    """Upsample 8 kHz PCM16 LE to 16 kHz via linear interpolation.

    NOT required for the Vobiz bridge — both sides are 8 kHz.
    Provided for future STT backends that need 16 kHz input.
    """
    samples = np.frombuffer(pcm8k, dtype="<i2").astype(np.float32)
    n_out = len(samples) * 2
    x_in = np.arange(len(samples), dtype=np.float32)
    x_out = np.linspace(0, len(samples) - 1, n_out)
    upsampled = np.interp(x_out, x_in, samples).astype(np.int16)
    return upsampled.astype("<i2").tobytes()


def resample_16k_to_8k(pcm16k: bytes) -> bytes:
    """Downsample 16 kHz PCM16 LE to 8 kHz via linear interpolation.

    NOT required for the Vobiz bridge. Symmetric counterpart to resample_8k_to_16k.
    """
    samples = np.frombuffer(pcm16k, dtype="<i2").astype(np.float32)
    n_out = max(1, len(samples) // 2)
    x_in = np.arange(len(samples), dtype=np.float32)
    x_out = np.linspace(0, len(samples) - 1, n_out)
    downsampled = np.interp(x_out, x_in, samples).astype(np.int16)
    return downsampled.astype("<i2").tobytes()
