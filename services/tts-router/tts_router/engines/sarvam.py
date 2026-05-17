from __future__ import annotations

import base64
import os

import httpx

from tts_router.audio import strip_wav_header
from tts_router.engines.base import TTSEngine
from tts_router.models import VoiceInfo

_API_KEY = os.getenv("SARVAM_API_KEY", "")
_URL = "https://api.sarvam.ai/text-to-speech"
_TIMEOUT = 10.0

_VOICES = [
    VoiceInfo(id="meera", name="Meera", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="pavithra", name="Pavithra", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="maitreyi", name="Maitreyi", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="arvind", name="Arvind", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="amol", name="Amol", lang="mr-IN", engine="sarvam_bulbul"),
]


class SarvamBulbulEngine(TTSEngine):
    """Sarvam Bulbul v3 — best Hinglish TTS, 30+ voices, API-only (no GPU).

    Output: 8kHz WAV (base64) → stripped to raw L16 PCM.
    Production: set SARVAM_API_KEY env var.
    """
    name = "sarvam_bulbul"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT) -> None:
        self._api_key = api_key or _API_KEY
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        payload = {
            "inputs": [text],
            "target_language_code": _lang_code(lang),
            "speaker": voice_id,
            "pitch": 0,
            "pace": 1.0,
            "loudness": 1.5,
            "speech_sample_rate": 8000,
            "enable_preprocessing": True,
            "model": "bulbul:v1",
        }
        headers = {"API-Subscription-Key": self._api_key}
        resp = await self._client.post(_URL, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        wav_b64 = body["audios"][0]
        wav = base64.b64decode(wav_b64)
        return strip_wav_header(wav)

    async def health_check(self) -> bool:
        return bool(self._api_key)

    def voices(self) -> list[VoiceInfo]:
        return _VOICES

    async def aclose(self) -> None:
        await self._client.aclose()


def _lang_code(lang: str) -> str:
    return {"hi": "hi-IN", "hi-en": "hi-IN", "en": "en-IN",
            "mr": "mr-IN", "ta": "ta-IN"}.get(lang, "hi-IN")
