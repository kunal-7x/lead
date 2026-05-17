from __future__ import annotations

import time
from abc import ABC, abstractmethod

from stt_router.models import STTResult


class STTEngine(ABC):
    """Abstract base for all STT engines."""

    name: str

    @abstractmethod
    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        """Transcribe raw PCM audio (8kHz or 16kHz L16 mono).

        Production engines upsample 8kHz→16kHz internally if required.
        Raises asyncio.TimeoutError on timeout (router marks engine unhealthy).
        """
        ...

    @abstractmethod
    async def health_check(self) -> bool:
        """Return True if the engine is reachable and ready."""
        ...

    def _make_result(
        self,
        text: str,
        confidence: float,
        lang: str,
        turn_start: float,
        latency_ms: int,
        is_final: bool = True,
    ) -> STTResult:
        return STTResult(
            text=text,
            is_final=is_final,
            confidence=confidence,
            language=lang,
            engine_used=self.name,
            latency_ms=latency_ms,
            turn_start_ts=turn_start,
            turn_end_ts=time.time() if is_final else None,
        )
