from __future__ import annotations

from abc import ABC, abstractmethod

from tts_router.models import VoiceInfo


class TTSEngine(ABC):
    name: str
    premium_only: bool = False

    @abstractmethod
    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        """Synthesize text. Returns L16 PCM 8kHz bytes.

        May return WAV bytes — caller strips header via audio.strip_wav_header().
        Raises asyncio.TimeoutError on timeout.
        """
        ...

    @abstractmethod
    async def health_check(self) -> bool: ...

    @abstractmethod
    def voices(self) -> list[VoiceInfo]: ...
