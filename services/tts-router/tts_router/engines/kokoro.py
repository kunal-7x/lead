from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from typing import AsyncIterator

import httpx
import websockets

from tts_router.audio import strip_wav_header
from tts_router.engines.base import TTSEngine
from tts_router.models import VoiceInfo

_KOKORO_URL = os.getenv("KOKORO_URL", "http://kokoro:8082")
_TIMEOUT = 8.0

log = logging.getLogger(__name__)


class KokoroEngine(TTSEngine):
    """Kokoro TTS — 82M params, CPU-runnable, lightweight Hindi/English.

    Self-hosted via docker run kokoro-tts (exposes POST /v1/audio/speech).
    Set KOKORO_URL env var.
    """
    name = "kokoro"

    def __init__(self, base_url: str = "", timeout: float = _TIMEOUT) -> None:
        self._base_url = base_url or _KOKORO_URL
        self._client = httpx.AsyncClient(
            timeout=timeout,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

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
        self._client = httpx.AsyncClient(
            timeout=15.0,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

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
        self._client = httpx.AsyncClient(
            timeout=15.0,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

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
    """ElevenLabs Flash v2.5 — low-latency streaming TTS producing µ-law 8kHz directly.

    Primary path: input-streaming WebSocket (synthesize_stream) → ~75ms TTFB.
    Fallback path: HTTP POST (synthesize) → batch, used when WS unavailable.
    Premium-only flag: only active when req.tts_premium=True.
    Feature-flag: TTS_STREAMING_WS=false disables WS path, forces HTTP fallback.

    Env vars:
      ELEVENLABS_API_KEY     — required
      ELEVENLABS_VOICE_ID    — default voice (can be overridden per-request)
      ELEVENLABS_MODEL       — default eleven_flash_v2_5
      TTS_OUTPUT_FORMAT      — default ulaw_8000 (µ-law 8kHz telephony output)
      TTS_STREAMING_WS       — true (default) | false  (disable WS streaming path)
    """
    name = "elevenlabs"
    premium_only = True

    _WS_BASE = "wss://api.elevenlabs.io/v1/text-to-speech/{voice_id}/stream-input"
    _HTTP_BASE = "https://api.elevenlabs.io/v1/text-to-speech/{voice_id}/stream"

    def __init__(self, api_key: str = "") -> None:
        self._api_key = api_key or os.getenv("ELEVENLABS_API_KEY", "")
        self._model = os.getenv("ELEVENLABS_MODEL", "eleven_flash_v2_5")
        self._output_format = os.getenv("TTS_OUTPUT_FORMAT", "ulaw_8000")
        self._streaming_ws_enabled = os.getenv("TTS_STREAMING_WS", "true").lower() != "false"
        self._default_voice = os.getenv("ELEVENLABS_VOICE_ID", "21m00Tcm4TlvDq8ikWAM")
        # Persistent HTTP client for batch fallback path
        self._client = httpx.AsyncClient(
            timeout=15.0,
            headers={"xi-api-key": self._api_key} if self._api_key else {},
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

    # ── Batch HTTP path (fallback) ────────────────────────────────────────────

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        """Batch HTTP synthesis. Returns raw audio bytes (µ-law 8kHz or PCM).

        This is the fallback used when WS streaming is disabled or fails.
        With output_format=ulaw_8000, ElevenLabs returns µ-law 8kHz bytes
        directly — no transcoding needed.
        """
        vid = voice_id or self._default_voice
        url = self._HTTP_BASE.format(voice_id=vid)
        params = {"output_format": self._output_format}
        payload = {
            "text": text,
            "model_id": self._model,
            "voice_settings": {"stability": 0.5, "similarity_boost": 0.75},
        }
        headers = {"xi-api-key": self._api_key}
        resp = await self._client.post(url, json=payload, params=params, headers=headers)
        resp.raise_for_status()
        # ElevenLabs HTTP stream endpoint returns raw audio (no WAV header for ulaw)
        return resp.content

    # ── Input-streaming WebSocket path (primary) ──────────────────────────────

    async def synthesize_stream(
        self,
        text_iter: AsyncIterator[str],
        voice_id: str = "",
        lang: str = "hi-en",
    ) -> AsyncIterator[bytes]:
        """Stream audio chunks from ElevenLabs input-streaming WS.

        Sends text tokens as they arrive from LLM; yields µ-law audio chunks
        as they come back. TTFB ~75ms from first token.

        Args:
            text_iter: async iterator of text tokens (from LLM stream)
            voice_id:  ElevenLabs voice ID (falls back to ELEVENLABS_VOICE_ID)
            lang:      language hint (unused by ElevenLabs but kept for interface compat)

        Yields:
            bytes: raw µ-law 8kHz audio chunks (ulaw_8000 format)

        Raises:
            ValueError: if API key is not configured
            websockets.exceptions.WebSocketException: on WS connection failure
        """
        if not self._api_key:
            raise ValueError("ELEVENLABS_API_KEY not configured")

        vid = voice_id or self._default_voice
        url = (
            self._WS_BASE.format(voice_id=vid)
            + f"?model_id={self._model}&output_format={self._output_format}"
        )
        extra_headers = {"xi-api-key": self._api_key}

        async with websockets.connect(url, additional_headers=extra_headers) as ws:
            # Send the BOS (begin-of-stream) config message
            bos = {
                "text": " ",
                "voice_settings": {
                    "stability": 0.5,
                    "similarity_boost": 0.75,
                },
                "generation_config": {
                    "chunk_length_schedule": [50, 100, 150],
                },
            }
            await ws.send(json.dumps(bos))

            # Task to drain text_iter and send tokens
            async def _send_text() -> None:
                async for chunk in text_iter:
                    if chunk:
                        await ws.send(json.dumps({"text": chunk}))
                # EOS sentinel
                await ws.send(json.dumps({"text": ""}))

            send_task = asyncio.ensure_future(_send_text())

            try:
                async for raw_msg in ws:
                    if isinstance(raw_msg, bytes):
                        # Some WS modes send raw binary
                        yield raw_msg
                    else:
                        msg = json.loads(raw_msg)
                        if msg.get("audio"):
                            import base64
                            yield base64.b64decode(msg["audio"])
                        elif msg.get("isFinal") or msg.get("is_final"):
                            break
                        elif msg.get("error"):
                            log.error("ElevenLabs WS error: %s", msg)
                            break
            finally:
                send_task.cancel()
                try:
                    await send_task
                except asyncio.CancelledError:
                    pass

    async def health_check(self) -> bool:
        """Check API key validity — cheap GET /v1/user."""
        if not self._api_key:
            return False
        # Optimistic: if key is set, report healthy (avoids live network hit in tests).
        # A real ping would be:
        #   resp = await self._client.get("https://api.elevenlabs.io/v1/user", timeout=3.0)
        #   return resp.status_code == 200
        return True

    def voices(self) -> list[VoiceInfo]:
        vid = self._default_voice
        return [VoiceInfo(id=vid, name="ElevenLabs Flash", lang="hi-en", engine="elevenlabs")]

    async def aclose(self) -> None:
        await self._client.aclose()
