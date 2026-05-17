from __future__ import annotations

import os
import time

import httpx

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult

_GROQ_API_KEY = os.getenv("GROQ_API_KEY", "")
_GROQ_URL = "https://api.groq.com/openai/v1/audio/transcriptions"
_TIMEOUT_S = 12.0


class GroqWhisperEngine(STTEngine):
    """Groq Whisper API — last-resort fallback when self-hosted engines are down.

    Production: set GROQ_API_KEY env var.
    Uses Groq's ultra-fast Whisper inference (~200ms on their infra).
    """

    name = "groq_whisper"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT_S) -> None:
        self._api_key = api_key or _GROQ_API_KEY
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        t0 = time.time()
        from stt_router.engines.sarvam import _wrap_pcm_wav
        wav = _wrap_pcm_wav(audio, sample_rate=8000)

        headers = {"Authorization": f"Bearer {self._api_key}"}
        files = {"file": ("audio.wav", wav, "audio/wav")}
        data = {"model": "whisper-large-v3", "language": _lang_code(lang)}

        resp = await self._client.post(_GROQ_URL, headers=headers, files=files, data=data)
        resp.raise_for_status()
        body = resp.json()
        text = body.get("text", "")
        latency_ms = int((time.time() - t0) * 1000)
        return self._make_result(text, 0.80, lang, t0, latency_ms)

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.groq.com/openai/v1/models",
                headers={"Authorization": f"Bearer {self._api_key}"},
                timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()


def _lang_code(lang: str) -> str:
    mapping = {"hi": "hi", "hi-en": "hi", "en": "en"}
    return mapping.get(lang, "hi")
