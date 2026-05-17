from __future__ import annotations

import os
import time

import httpx

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult

_INDICCONFORMER_URL = os.getenv("INDICCONFORMER_URL", "http://indicconformer:8080")
_TIMEOUT_S = 8.0


class IndicConformerEngine(STTEngine):
    """IndicConformer 600M multilingual — self-hosted, CPU-only secondary engine.

    Production: docker run ai4bharat/vexyl-stt:latest (exposes POST /transcribe).
    Set INDICCONFORMER_URL env var (default: http://indicconformer:8080).
    Supports 22 Indian languages, ~2GB RAM, real-time on 4-core CPU.
    Accepts 16kHz PCM (8kHz is upsampled internally).
    """

    name = "indicconformer"

    def __init__(self, base_url: str = "", timeout: float = _TIMEOUT_S) -> None:
        self._base_url = base_url or _INDICCONFORMER_URL
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        t0 = time.time()
        resp = await self._client.post(
            f"{self._base_url}/transcribe",
            content=audio,
            headers={"Content-Type": "audio/pcm", "X-Language": lang},
        )
        resp.raise_for_status()
        body = resp.json()
        text = body.get("text", "")
        confidence = float(body.get("confidence", 0.85))
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
