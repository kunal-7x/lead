from __future__ import annotations

import os
import time

import httpx

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult

_FASTER_WHISPER_URL = os.getenv("FASTER_WHISPER_URL", "http://faster-whisper:8081")
_TIMEOUT_S = 15.0


class FasterWhisperEngine(STTEngine):
    """faster-whisper large-v3 — self-hosted GPU optional, English + multilingual.

    Production: GPU on RunPod/Vast.ai when needed (~8hr/day).
    Set FASTER_WHISPER_URL env var.
    Falls back to CPU mode if no GPU detected (slower but functional).
    """

    name = "faster_whisper"

    def __init__(self, base_url: str = "", timeout: float = _TIMEOUT_S) -> None:
        self._base_url = base_url or _FASTER_WHISPER_URL
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        t0 = time.time()
        resp = await self._client.post(
            f"{self._base_url}/v1/transcribe",
            content=audio,
            headers={"Content-Type": "audio/pcm", "X-Language": lang},
        )
        resp.raise_for_status()
        body = resp.json()
        text = body.get("text", "")
        confidence = float(body.get("confidence", 0.80))
        latency_ms = int((time.time() - t0) * 1000)
        return self._make_result(text, confidence, lang, t0, latency_ms)

    async def health_check(self) -> bool:
        try:
            resp = await self._client.get(f"{self._base_url}/health", timeout=2.0)
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()
