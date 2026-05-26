from __future__ import annotations

import time
from typing import AsyncIterator


class FakeSarvamStreamingEngine:
    """Fake SarvamStreamingEngine for tests — returns canned events without network."""

    name = "sarvam_streaming"

    def __init__(
        self,
        transcript: str = "namaste test",
        confidence: float = 0.90,
        latency_ms: int = 302,
        error: str | None = None,
    ) -> None:
        self.transcript = transcript
        self.confidence = confidence
        self.latency_ms = latency_ms
        self.error = error
        self.call_count = 0

    async def stream_utterance(
        self,
        audio_frames,
        lang: str = "hi-en",
        session_id: str = "",
        chunk_size: int = 4000,
        audio_format: str = "pcm16",
    ) -> AsyncIterator[dict]:
        self.call_count += 1
        # Collect bytes if async iterator
        if not isinstance(audio_frames, bytes):
            parts = []
            async for frame in audio_frames:
                parts.append(frame)
        t_now = time.time()
        if self.error:
            yield {"type": "error", "message": self.error}
            return
        yield {"type": "interim", "text": "", "ts": t_now}
        yield {
            "type": "final",
            "text": self.transcript,
            "confidence": self.confidence,
            "latency_ms": self.latency_ms,
            "engine_used": self.name,
            "ts": t_now,
        }

    async def health_check(self) -> bool:
        return True
