from __future__ import annotations

import os

import httpx

from tts_router.audio import strip_wav_header
from tts_router.engines.base import TTSEngine
from tts_router.models import VoiceInfo

_KOKORO_URL = os.getenv("KOKORO_URL", "http://kokoro:8082")
_TIMEOUT = 8.0


class KokoroEngine(TTSEngine):
    """Kokoro TTS — 82M params, CPU-runnable, lightweight Hindi/English.

    Self-hosted via docker run kokoro-tts (exposes POST /v1/audio/speech).
    Set KOKORO_URL env var.
    """
    name = "kokoro"

    def __init__(self, base_url: str = "", timeout: float = _TIMEOUT) -> None:
        self._base_url = base_url or _KOKORO_URL
        self._client = httpx.AsyncClient(timeout=timeout)

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        payload = {"model": "kokoro", "input": text, "voice": voice_id,
                   "response_format": "pcm", "speed": 1.0}
        resp = await self._client.post(f"{self._base_url}/v1/audio/speech", json=payload)
        resp.raise_for_status()
        return strip_wav_header(resp.content)

    async def health_check(self) -> bool:
        try:
            resp = await self._client.get(f"{self._base_url}/health", timeout=2.0)
            return resp.status_code == 200
        except Exception:
            return False

    def voices(self) -> list[VoiceInfo]:
        return [VoiceInfo(id="af_heart", name="Heart", lang="hi-en", engine="kokoro"),
                VoiceInfo(id="hf_alpha", name="Alpha", lang="hi-en", engine="kokoro")]

    async def aclose(self) -> None:
        await self._client.aclose()


class IndicParlerEngine(TTSEngine):
    """Indic Parler-TTS — 21 Indian languages, GPU optional, self-hosted.

    Production: GPU on RunPod. Set INDIC_PARLER_URL env var.
    """
    name = "indic_parler"

    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or os.getenv("INDIC_PARLER_URL", "http://indic-parler:8083")
        self._client = httpx.AsyncClient(timeout=15.0)

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        resp = await self._client.post(f"{self._base_url}/synthesize",
                                       json={"text": text, "voice": voice_id, "lang": lang})
        resp.raise_for_status()
        return strip_wav_header(resp.content)

    async def health_check(self) -> bool:
        try:
            resp = await self._client.get(f"{self._base_url}/health", timeout=2.0)
            return resp.status_code == 200
        except Exception:
            return False

    def voices(self) -> list[VoiceInfo]:
        return [VoiceInfo(id="default", name="Default", lang="hi-en", engine="indic_parler")]

    async def aclose(self) -> None:
        await self._client.aclose()


class IndicF5Engine(TTSEngine):
    """IndicF5 — voice cloning, 11 Indian languages, GPU optional."""
    name = "indicf5"

    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or os.getenv("INDICF5_URL", "http://indicf5:8084")
        self._client = httpx.AsyncClient(timeout=15.0)

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        resp = await self._client.post(f"{self._base_url}/synthesize",
                                       json={"text": text, "voice_ref": voice_id, "lang": lang})
        resp.raise_for_status()
        return strip_wav_header(resp.content)

    async def health_check(self) -> bool:
        try:
            resp = await self._client.get(f"{self._base_url}/health", timeout=2.0)
            return resp.status_code == 200
        except Exception:
            return False

    def voices(self) -> list[VoiceInfo]:
        return [VoiceInfo(id="custom", name="Custom Clone", lang="hi-en", engine="indicf5")]

    async def aclose(self) -> None:
        await self._client.aclose()


class ElevenLabsEngine(TTSEngine):
    """ElevenLabs — premium quality, only for tenants with tts_premium=true."""
    name = "elevenlabs"
    premium_only = True

    def __init__(self, api_key: str = "") -> None:
        self._api_key = api_key or os.getenv("ELEVENLABS_API_KEY", "")
        self._client = httpx.AsyncClient(timeout=15.0)

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        url = f"https://api.elevenlabs.io/v1/text-to-speech/{voice_id}/stream"
        payload = {"text": text, "model_id": "eleven_multilingual_v2",
                   "voice_settings": {"stability": 0.5, "similarity_boost": 0.75}}
        headers = {"xi-api-key": self._api_key}
        resp = await self._client.post(url, json=payload, headers=headers)
        resp.raise_for_status()
        return strip_wav_header(resp.content)

    async def health_check(self) -> bool:
        """Real ping: list user — cheap and authenticates the key."""
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.elevenlabs.io/v1/user",
                headers={"xi-api-key": self._api_key}, timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    def voices(self) -> list[VoiceInfo]:
        return [VoiceInfo(id="21m00Tcm4TlvDq8ikWAM", name="Rachel", lang="en", engine="elevenlabs")]

    async def aclose(self) -> None:
        await self._client.aclose()
