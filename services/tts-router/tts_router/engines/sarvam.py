from __future__ import annotations

import asyncio
import base64
import json
import logging
import os
from typing import AsyncIterator

import httpx

from tts_router.audio import strip_wav_header
from tts_router.engines.base import TTSEngine
from tts_router.models import VoiceInfo

log = logging.getLogger(__name__)

_API_KEY = os.getenv("SARVAM_API_KEY", "").strip()  # strip \r\n from Windows .env files
_URL = "https://api.sarvam.ai/text-to-speech"
# Streaming WS endpoint (bulbul:v3). Correct endpoint: /ws (NOT /stream).
# model goes into the config JSON frame — NOT as a URL query param.
_WS_URL = "wss://api.sarvam.ai/text-to-speech/ws"
_TIMEOUT = 10.0
# Streaming sample rate: bulbul:v3 WS defaults to 24000 Hz PCM16. We request the
# Sarvam default and resample to 8 kHz on the worker side (audio output is L16 PCM).
_STREAM_SAMPLE_RATE = int(os.getenv("SARVAM_STREAM_SAMPLE_RATE", "24000"))
# Override TTS model via env (default bulbul:v3)
_TTS_MODEL = os.getenv("SARVAM_TTS_MODEL", "bulbul:v3")

# bulbul:v3 uses different speaker names; v2 retains the old set.
# Default speaker: priya (natural female Hindi voice, available in v3).
# If model is v2, fall back to anushka unless explicitly overridden.
_TTS_MODEL_IS_V3 = _TTS_MODEL.endswith(":v3") or _TTS_MODEL == "bulbul:v3"
_DEFAULT_SPEAKER_V3 = "priya"
_DEFAULT_SPEAKER_V2 = "anushka"
_DEFAULT_SPEAKER_INFERRED = _DEFAULT_SPEAKER_V3 if _TTS_MODEL_IS_V3 else _DEFAULT_SPEAKER_V2
_SARVAM_TTS_SPEAKER = os.getenv("SARVAM_TTS_SPEAKER", _DEFAULT_SPEAKER_INFERRED)

# bulbul:v3 speaker list (all available; female recommended for Hindi telecaller: priya, neha, pooja, simran, kavya)
_VOICES_V3 = [
    VoiceInfo(id="priya", name="Priya", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="neha", name="Neha", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="pooja", name="Pooja", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="simran", name="Simran", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="kavya", name="Kavya", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="ishita", name="Ishita", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="shreya", name="Shreya", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="ritu", name="Ritu", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="tanya", name="Tanya", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="suhani", name="Suhani", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="aditya", name="Aditya", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="rahul", name="Rahul", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="rohan", name="Rohan", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="kabir", name="Kabir", lang="hi-IN", engine="sarvam_bulbul"),
]
# bulbul:v2 legacy speaker list
_VOICES_V2 = [
    VoiceInfo(id="anushka", name="Anushka", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="manisha", name="Manisha", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="vidya", name="Vidya", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="arya", name="Arya", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="abhilash", name="Abhilash", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="karun", name="Karun", lang="hi-IN", engine="sarvam_bulbul"),
    VoiceInfo(id="hitesh", name="Hitesh", lang="hi-IN", engine="sarvam_bulbul"),
]

_VOICES = _VOICES_V3 if _TTS_MODEL_IS_V3 else _VOICES_V2
_DEFAULT_SPEAKER = _SARVAM_TTS_SPEAKER
_VALID_SPEAKERS = {v.id for v in _VOICES}


class SarvamBulbulEngine(TTSEngine):
    """Sarvam Bulbul v3 — best Hinglish TTS, 30+ voices, API-only (no GPU).

    Output: 8kHz WAV (base64) → stripped to raw L16 PCM.
    Production: set SARVAM_API_KEY env var.
    """
    name = "sarvam_bulbul"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT) -> None:
        self._api_key = (api_key or _API_KEY).strip()  # strip \r\n from Windows .env
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def synthesize(self, text: str, voice_id: str, lang: str) -> bytes:
        speaker = voice_id if voice_id in _VALID_SPEAKERS else _DEFAULT_SPEAKER
        payload: dict = {
            "inputs": [text],
            "target_language_code": _lang_code(lang),
            "speaker": speaker,
            "pace": 1.0,
            "speech_sample_rate": 8000,
            "enable_preprocessing": True,
            "model": _TTS_MODEL,
        }
        # bulbul:v3 rejects pitch and loudness parameters — only include for v2
        if not _TTS_MODEL_IS_V3:
            payload["pitch"] = 0
            payload["loudness"] = 1.5
        headers = {"API-Subscription-Key": self._api_key}
        resp = await self._client.post(_URL, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        wav_b64 = body["audios"][0]
        wav = base64.b64decode(wav_b64)
        return strip_wav_header(wav)

    async def synthesize_stream(
        self, text: str, voice_id: str, lang: str
    ) -> AsyncIterator[bytes]:
        """Stream TTS audio from Sarvam Bulbul:v3 over WebSocket.

        LATENCY: first-audio ~0.3s vs ~2.2s for the blocking REST synthesize().
        We open one WS per call here; the worker reuses a single WS per session
        via its own client wrapper (see voice_agent.clients).

        Protocol (bulbul:v3 streaming, per tts_sarvam_research.md):
          1. open WS to wss://api.sarvam.ai/text-to-speech/ws  (NO query params)
             auth header: API-Subscription-Key: <SARVAM_API_KEY>
          2. send {"type":"config","data":{...}} FIRST — model bulbul:v3 IN config,
             language hi-IN, speaker priya, pace 1.0, temperature 0.6,
             output_audio_codec linear16, sample_rate 24000.
             bulbul:v3 REJECTS pitch/loudness — never send them.
          3. send {"type":"text","data":{"text": <chunk>}}
          4. send {"type":"flush"} to force synthesis of buffered text
          5. receive {"type":"audio","data":{"audio": <base64 PCM16>}} chunks
          6. completion event ends the stream

        Yields:
            bytes: raw L16 PCM16 little-endian at _STREAM_SAMPLE_RATE (default 24kHz).
            The worker resamples 24k→8k before feeding send_audio (which µ-law-encodes).
            Output format mirrors synthesize() (L16 PCM) — NOT µ-law, to avoid
            double-encoding in send_audio.

        Raises:
            ValueError: if API key missing.
            Exception: on WS failure — caller falls back to batch synthesize().
        """
        if not self._api_key:
            raise ValueError("SARVAM_API_KEY not configured")
        try:
            import websockets  # type: ignore
        except ImportError as exc:  # pragma: no cover
            raise RuntimeError("websockets package not installed; streaming TTS unavailable") from exc

        # Sarvam WS /ws endpoint defaults to bulbul:v2 regardless of model field
        # in config (confirmed empirically: "model" field in config is silently ignored).
        # WS streaming only supports v2 speakers; fall back to anushka for v3 voices.
        _WS_VALID_SPEAKERS = {v.id for v in _VOICES_V2}
        speaker = voice_id if voice_id in _WS_VALID_SPEAKERS else "anushka"

        # URL is plain (no query params, no path model) — the WS endpoint controls model server-side.
        url = _WS_URL
        headers = {"API-Subscription-Key": self._api_key}

        config = {
            "type": "config",
            "data": {
                # NOTE: do NOT include "model" — Sarvam WS ignores it and defaults to
                # bulbul:v2. Using v2-compatible speaker (anushka) for WS path.
                "target_language_code": _lang_code(lang),
                "speaker": speaker,
                "pace": 1.0,
                "temperature": 0.6,
                "enable_preprocessing": True,
                "output_audio_codec": "linear16",
                "min_buffer_size": 50,
                "max_chunk_length": 250,
                "sample_rate": _STREAM_SAMPLE_RATE,
            },
        }

        try:
            ws_cm = websockets.connect(
                url, additional_headers=headers, open_timeout=3, close_timeout=2
            )
        except Exception as exc:
            log.error("sarvam_ws_connect_failed url=%s error=%r", url, exc)
            raise

        try:
            async with ws_cm as ws:
                await ws.send(json.dumps(config))
                await ws.send(json.dumps({"type": "text", "data": {"text": text,
                                                                    "send_completion_event": True}}))
                await ws.send(json.dumps({"type": "flush"}))

                try:
                    async for raw in ws:
                        if isinstance(raw, bytes):
                            # Some deployments stream raw PCM frames directly.
                            yield raw
                            continue
                        try:
                            msg = json.loads(raw)
                        except (ValueError, TypeError):
                            continue
                        mtype = msg.get("type", "")
                        if mtype in ("audio", "audio_chunk"):
                            data_field = msg.get("data", {})
                            audio_b64 = data_field.get("audio") or data_field.get("audio_chunk") or ""
                            if audio_b64:
                                yield base64.b64decode(audio_b64)
                        elif mtype in ("flush_done", "done", "complete", "completion"):
                            break
                        elif mtype == "error":
                            err_data = msg.get("data") or msg.get("message") or msg
                            err_code = (msg.get("data") or {}).get("code") if isinstance(msg.get("data"), dict) else None
                            log.error(
                                "sarvam_ws_error_frame type=error code=%s body=%r", err_code, err_data
                            )
                            raise RuntimeError(f"sarvam_stream_error code={err_code}: {err_data}")
                except websockets.exceptions.ConnectionClosed:
                    # Sarvam WS may close connection after streaming audio without
                    # sending a done/completion frame — treat close as normal end.
                    log.debug("sarvam_ws_connection_closed_normally")
        except websockets.exceptions.InvalidStatus as exc:
            # Capture the full HTTP response body from 403/4xx rejections.
            status = getattr(exc, "status_code", None) or getattr(getattr(exc, "response", None), "status_code", None)
            body = getattr(getattr(exc, "response", None), "body", b"") or b""
            log.error(
                "sarvam_ws_upgrade_failed status=%s url=%s body=%r headers=%r",
                status, url, body[:500], dict(getattr(getattr(exc, "response", None), "headers", {})),
            )
            raise

    async def health_check(self) -> bool:
        """Real ping: tiny TTS call. Cached at the router layer (5s TTL)."""
        if not self._api_key:
            return False
        try:
            payload: dict = {
                "inputs": ["ok"],
                "target_language_code": "hi-IN",
                "speaker": _DEFAULT_SPEAKER,
                "speech_sample_rate": 8000,
                "model": _TTS_MODEL,
            }
            if not _TTS_MODEL_IS_V3:
                payload["pitch"] = 0
                payload["loudness"] = 1.5
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


# Sarvam WS 408 idle-timeout fires at ~30s. Send WS-level ping every 20s to keep alive.
_KEEPALIVE_INTERVAL_S = float(os.getenv("SARVAM_WS_KEEPALIVE_INTERVAL", "20"))


class SarvamStreamingSession:
    """Persistent Sarvam WS session that survives across multiple utterances.

    Maintains one WebSocket connection per caller session; sends periodic WS-level
    pings so the Sarvam server does not close with 408 idle-timeout between turns.

    Usage (from tts_sarvam_stream_ws app endpoint)::

        async with SarvamStreamingSession(engine) as session:
            while True:
                text = await get_next_utterance()
                async for pcm in session.synthesize(text, voice_id, lang):
                    yield pcm

    If the WS drops (408, network error) it reconnects transparently before the
    next utterance — single reconnect per utterance, so first-chunk latency is the
    same as a fresh connect but we avoid paying the reconnect cost every turn.
    """

    def __init__(self, engine: SarvamBulbulEngine) -> None:
        self._engine = engine
        self._ws = None  # active websockets connection (or None)
        self._ws_cm = None  # the context manager; kept open while _ws is alive
        self._ping_task: asyncio.Task | None = None
        self._lang: str = "hi-en"
        self._voice_id: str = ""

    async def __aenter__(self) -> "SarvamStreamingSession":
        return self

    async def __aexit__(self, *exc) -> None:
        await self._close()

    async def _close(self) -> None:
        if self._ping_task is not None:
            self._ping_task.cancel()
            try:
                await self._ping_task
            except (asyncio.CancelledError, Exception):
                pass
            self._ping_task = None
        if self._ws_cm is not None:
            try:
                await self._ws_cm.__aexit__(None, None, None)
            except Exception:
                pass
            self._ws_cm = None
        self._ws = None

    async def _connect(self, voice_id: str, lang: str) -> None:
        """Open a new Sarvam WS (via async CM), send config, start keepalive ping loop."""
        try:
            import websockets  # type: ignore
        except ImportError as exc:
            raise RuntimeError("websockets not installed") from exc

        _WS_VALID_SPEAKERS = {v.id for v in _VOICES_V2}
        speaker = voice_id if voice_id in _WS_VALID_SPEAKERS else "anushka"
        headers = {"API-Subscription-Key": self._engine._api_key}
        config = {
            "type": "config",
            "data": {
                "target_language_code": _lang_code(lang),
                "speaker": speaker,
                "pace": 1.0,
                "temperature": 0.6,
                "enable_preprocessing": True,
                "output_audio_codec": "linear16",
                "min_buffer_size": 50,
                "max_chunk_length": 250,
                "sample_rate": _STREAM_SAMPLE_RATE,
            },
        }
        # websockets.connect() returns an async CM; enter it and hold it open so
        # the connection persists across turns (not closed when we exit the CM block).
        cm = websockets.connect(
            _WS_URL, additional_headers=headers, open_timeout=3, close_timeout=2
        )
        ws = await cm.__aenter__()
        await ws.send(json.dumps(config))
        self._ws_cm = cm
        self._ws = ws
        self._lang = lang
        self._voice_id = voice_id
        # Start background ping task
        if self._ping_task is not None:
            self._ping_task.cancel()
        self._ping_task = asyncio.ensure_future(self._keepalive_loop())
        log.debug("sarvam_ws_session_connected speaker=%s lang=%s", speaker, lang)

    async def _keepalive_loop(self) -> None:
        """Send WS-level pings every _KEEPALIVE_INTERVAL_S to prevent 408."""
        try:
            while True:
                await asyncio.sleep(_KEEPALIVE_INTERVAL_S)
                if self._ws is None:
                    return
                try:
                    await self._ws.ping()
                    log.debug("sarvam_ws_keepalive_ping sent")
                except Exception as exc:
                    log.warning("sarvam_ws_keepalive_ping failed: %r", exc)
                    self._ws = None   # mark as dead; _ensure_connected will reconnect
                    self._ws_cm = None
                    return
        except asyncio.CancelledError:
            pass

    async def _ensure_connected(self, voice_id: str, lang: str) -> None:
        """Reconnect if WS is dead or config changed."""
        if self._ws is None or self._lang != lang or self._voice_id != voice_id:
            await self._close()
            await self._connect(voice_id, lang)

    async def synthesize(
        self, text: str, voice_id: str, lang: str
    ) -> AsyncIterator[bytes]:
        """Synthesize text on the persistent WS; reconnect once if WS is dead."""
        for attempt in range(2):
            try:
                await self._ensure_connected(voice_id, lang)
                ws = self._ws
                await ws.send(json.dumps({"type": "text", "data": {"text": text,
                                                                    "send_completion_event": True}}))
                await ws.send(json.dumps({"type": "flush"}))
                try:
                    import websockets  # type: ignore
                    async for raw in ws:
                        if isinstance(raw, bytes):
                            yield raw
                            continue
                        try:
                            msg = json.loads(raw)
                        except (ValueError, TypeError):
                            continue
                        mtype = msg.get("type", "")
                        if mtype in ("audio", "audio_chunk"):
                            data_field = msg.get("data", {})
                            audio_b64 = (data_field.get("audio") or
                                         data_field.get("audio_chunk") or "")
                            if audio_b64:
                                yield base64.b64decode(audio_b64)
                        elif mtype in ("flush_done", "done", "complete", "completion"):
                            break
                        elif mtype == "error":
                            err_data = msg.get("data") or msg.get("message") or msg
                            err_code = (
                                (msg.get("data") or {}).get("code")
                                if isinstance(msg.get("data"), dict) else None
                            )
                            raise RuntimeError(f"sarvam_stream_error code={err_code}: {err_data}")
                except websockets.exceptions.ConnectionClosed as exc:
                    # Sarvam closed after audio (normal) or mid-stream (error).
                    # Treat close as normal end; mark WS dead so next utterance reconnects.
                    log.debug("sarvam_ws_session_closed_during_stream: %r", exc)
                    self._ws = None
                    self._ws_cm = None
                    if self._ping_task:
                        self._ping_task.cancel()
                        self._ping_task = None
                return  # success
            except Exception as exc:
                log.warning("sarvam_ws_session attempt=%d error=%r", attempt, exc)
                # Mark dead and retry once
                self._ws = None
                self._ws_cm = None
                if self._ping_task:
                    self._ping_task.cancel()
                    self._ping_task = None
                if attempt == 1:
                    raise  # both attempts failed — caller falls back to REST


def _lang_code(lang: str) -> str:
    """Map lang tag to Sarvam target_language_code.

    hi, hi-IN, hi-en all map to hi-IN (pure Hindi rendering —
    Devanagari input is rendered naturally by bulbul:v2 at hi-IN).
    """
    return {"hi": "hi-IN", "hi-IN": "hi-IN", "hi-en": "hi-IN", "en": "en-IN",
            "en-IN": "en-IN", "mr": "mr-IN", "ta": "ta-IN"}.get(lang, "hi-IN")
