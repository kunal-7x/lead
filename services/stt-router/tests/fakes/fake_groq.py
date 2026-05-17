from __future__ import annotations

import time

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult


class FakeGroqWhisperEngine(STTEngine):
    name = "groq_whisper"

    def __init__(
        self,
        transcript: str = "groq whisper fallback transcript",
        confidence: float = 0.80,
        healthy: bool = True,
    ) -> None:
        self.transcript = transcript
        self.confidence = confidence
        self._healthy = healthy
        self.call_count = 0

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        self.call_count += 1
        t0 = time.time()
        return self._make_result(self.transcript, self.confidence, lang, t0, 200)

    async def health_check(self) -> bool:
        return self._healthy
