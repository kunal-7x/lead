from __future__ import annotations

import time

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult


class FakeIndicConformerEngine(STTEngine):
    """Fake IndicConformer engine for tests — returns fixture transcripts."""

    name = "indicconformer"

    def __init__(
        self,
        transcript: str = "hello this is indicconformer fallback",
        confidence: float = 0.87,
        latency_ms: int = 300,
        healthy: bool = True,
    ) -> None:
        self.transcript = transcript
        self.confidence = confidence
        self.latency_ms = latency_ms
        self._healthy = healthy
        self.call_count = 0

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        self.call_count += 1
        t0 = time.time()
        return self._make_result(self.transcript, self.confidence, lang, t0, self.latency_ms)

    async def health_check(self) -> bool:
        return self._healthy
