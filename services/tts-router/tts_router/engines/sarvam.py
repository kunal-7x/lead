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
# Override TTS model via env (default bulbul:v2)
_TTS_MODEL = os.getenv("SARVAM_TTS_MODEL", "bulbul:v2")

_VOICES = [
    VoiceInfo(id="anushka", name="Anushka", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="manisha", name="Manisha", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="vidya", name="Vidya", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="arya", name="Arya", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="abhilash", name="Abhilash", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="karun", name="Karun", lang="hi-en", engine="sarvam_bulbul"),
    VoiceInfo(id="hitesh", name="Hitesh", lang="hi-en", engine="sarvam_bulbul"),
]

_DEFAULT_SPEAKER = "anushka"
_VALID_SPEAKERS = {v.id for v in _VOICES}


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
        speaker = voice_id if voice_id in _VALID_SPEAKERS else _DEFAULT_SPEAKER
        payload = {
            "inputs": [text],
            "target_language_code": _lang_code(lang),
            "speaker": speaker,
            "pitch": 0,
            "pace": 1.0,
            "loudness": 1.5,
            "speech_sample_rate": 8000,
            "enable_preprocessing": True,
            "model": _TTS_MODEL,
        }
        headers = {"API-Subscription-Key": self._api_key}
        resp = await self._client.post(_URL, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        wav_b64 = body["audios"][0]
        wav = base64.b64decode(wav_b64)
        return strip_wav_header(wav)

    async def health_check(self) -> bool:
        """Real ping: tiny TTS call. Cached at the router layer (5s TTL)."""
        if not self._api_key:
            return False
        try:
            payload = {
                "inputs": ["ok"],
                "target_language_code": "hi-IN",
                "speaker": _DEFAULT_SPEAKER,
                "speech_sample_rate": 8000,
                "model": _TTS_MODEL,
            }
            resp = await self._client.post(
                _URL, json=payload,
                headers={"API-Subscription-Key": self._api_key}, timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    def voices(self) -> list[VoiceInfo]:
        return _VOICES

    async def aclose(self) -> None:
        await self._client.aclose()


def _lang_code(lang: str) -> str:
    return {"hi": "hi-IN", "hi-en": "hi-IN", "en": "en-IN",
            "mr": "mr-IN", "ta": "ta-IN"}.get(lang, "hi-IN")
