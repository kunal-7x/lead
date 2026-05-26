from __future__ import annotations

import asyncio
import time

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult


class FakeSarvamEngine(STTEngine):
    """Fake Sarvam engine for tests — returns canned transcripts."""

    name = "sarvam"

    def __init__(
        self,
        transcript: str = "नमस्ते, मैं आपकी कैसे मदद कर सकता हूँ",
        confidence: float = 0.92,
        latency_ms: int = 150,
        timeout: bool = False,
        healthy: bool = True,
    ) -> None:
        self.transcript = transcript
        self.confidence = confidence
        self.latency_ms = latency_ms
        self._timeout = timeout
        self._healthy = healthy
        self.call_count = 0

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        self.call_count += 1
        if self._timeout:
            await asyncio.sleep(60)  # will be cancelled by router timeout (any value)
        t0 = time.time()
        return self._make_result(self.transcript, self.confidence, lang, t0, self.latency_ms)

    async def health_check(self) -> bool:
        return self._healthy
