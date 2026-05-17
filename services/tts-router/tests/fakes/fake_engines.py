from __future__ import annotations

import asyncio
import struct

from tts_router.engines.base import TTSEngine
from tts_router.models import VoiceInfo


def _make_pcm(n_samples: int = 160) -> bytes:
    """Make n_samples of silent L16 PCM at 8kHz (20ms default)."""
    return struct.pack(f"<{n_samples}h", *([0] * n_samples))


class FakeSarvamTTS(TTSEngine):
    name = "sarvam_bulbul"
    premium_only = False

    def __init__(self, audio: bytes | None = None, timeout: bool = False, healthy: bool = True) -> None:
        self._audio = audio or _make_pcm(800)  # 100ms of PCM
        self._timeout = timeout
        self._healthy = healthy
        self.call_count = 0

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        self.call_count += 1
        if self._timeout:
            await asyncio.sleep(60)
        return self._audio

    async def health_check(self) -> bool:
        return self._healthy

    def voices(self) -> list[VoiceInfo]:
        return [VoiceInfo(id="meera", name="Meera", lang="hi-en", engine="sarvam_bulbul")]


class FakeKokoroTTS(TTSEngine):
    name = "kokoro"
    premium_only = False

    def __init__(self, audio: bytes | None = None, healthy: bool = True) -> None:
        self._audio = audio or _make_pcm(800)
        self._healthy = healthy
        self.call_count = 0

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        self.call_count += 1
        return self._audio

    async def health_check(self) -> bool:
        return self._healthy

    def voices(self) -> list[VoiceInfo]:
        return [VoiceInfo(id="af_heart", name="Heart", lang="hi-en", engine="kokoro")]


class FakeElevenLabsTTS(TTSEngine):
    name = "elevenlabs"
    premium_only = True

    def __init__(self, audio: bytes | None = None, healthy: bool = True) -> None:
        self._audio = audio or _make_pcm(800)
        self._healthy = healthy
        self.call_count = 0

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        self.call_count += 1
        return self._audio

    async def health_check(self) -> bool:
        return self._healthy

    def voices(self) -> list[VoiceInfo]:
        return [VoiceInfo(id="rachel", name="Rachel", lang="en", engine="elevenlabs")]
