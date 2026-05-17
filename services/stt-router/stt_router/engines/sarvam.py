from __future__ import annotations

import os
import time

import httpx

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult

_SARVAM_API_KEY = os.getenv("SARVAM_API_KEY", "")
_BATCH_URL = "https://api.sarvam.ai/speech-to-text-translate"
_TIMEOUT_S = 10.0


class SarvamEngine(STTEngine):
    """Sarvam Saarika v2 — best Hinglish/Hindi accuracy, API-only (no GPU).

    Production: set SARVAM_API_KEY env var.
    Audio: upsample 8kHz→16kHz before sending (Sarvam requires 16kHz PCM).
    Streaming: wss://api.sarvam.ai/v1/realtime/stream (Phase 14).
    """

    name = "sarvam"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT_S) -> None:
        self._api_key = api_key or _SARVAM_API_KEY
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        t0 = time.time()
        # Upsample 8kHz → 16kHz by linear interpolation (simple 2x)
        pcm = _upsample_8k_to_16k(audio)

        headers = {"API-Subscription-Key": self._api_key}
        files = {"file": ("audio.wav", _wrap_pcm_wav(pcm, sample_rate=16000), "audio/wav")}
        data = {"language_code": _lang_code(lang), "model": "saarika:v2"}

        resp = await self._client.post(_BATCH_URL, headers=headers, files=files, data=data)
        resp.raise_for_status()
        body = resp.json()

        text = body.get("transcript", "")
        confidence = float(body.get("confidence", 0.9))
        latency_ms = int((time.time() - t0) * 1000)
        return self._make_result(text, confidence, lang, t0, latency_ms)

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.sarvam.ai/v1/health",
                headers={"API-Subscription-Key": self._api_key},
                timeout=3.0,
            )
            return resp.status_code < 500
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()


def _lang_code(lang: str) -> str:
    mapping = {"hi": "hi-IN", "hi-en": "hi-IN", "en": "en-IN"}
    return mapping.get(lang, "hi-IN")


def _upsample_8k_to_16k(pcm8k: bytes) -> bytes:
    """Double sample rate by linear interpolation (8kHz → 16kHz)."""
    import struct
    samples = struct.unpack(f"<{len(pcm8k)//2}h", pcm8k)
    out = []
    for i, s in enumerate(samples):
        out.append(s)
        # Interpolate between current and next sample
        nxt = samples[i + 1] if i + 1 < len(samples) else s
        out.append((s + nxt) // 2)
    return struct.pack(f"<{len(out)}h", *out)


def _wrap_pcm_wav(pcm: bytes, sample_rate: int = 16000, channels: int = 1, bits: int = 16) -> bytes:
    """Wrap raw L16 PCM bytes in a minimal WAV container."""
    import struct
    data_size = len(pcm)
    header = struct.pack(
        "<4sI4s4sIHHIIHH4sI",
        b"RIFF", 36 + data_size, b"WAVE",
        b"fmt ", 16, 1, channels, sample_rate,
        sample_rate * channels * bits // 8,
        channels * bits // 8, bits,
        b"data", data_size,
    )
    return header + pcm
