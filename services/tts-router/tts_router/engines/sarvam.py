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
# bulbul:v3 WS emits µ-law 8kHz DIRECTLY when output_audio_codec=mulaw +
# speech_sample_rate=8000 — no 24k→8k resample needed. The streaming session
# decodes µ-law→PCM16 8k before yielding so the worker contract (PCM16 8k that
# send_audio µ-law-encodes) is preserved with NO double-encode.
_STREAM_SAMPLE_RATE = 8000
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

# Frame types that signal a REAL end of synthesis for the current utterance.
# Only these mark the stream "completed". A ConnectionClosed / idle / error
# BEFORE one of these means the audio was TRUNCATED — never treat as success.
_COMPLETION_TYPES = ("flush_done", "done", "complete", "completion")


def _build_stream_config(speaker: str, lang: str) -> dict:
    """bulbul:v3 streaming config frame.

    model goes IN the config (NOT the URL — URL-model caused the old 403).
    output_audio_codec=mulaw + speech_sample_rate=8000 → Sarvam emits µ-law 8k
    DIRECTLY. v3 REJECTS pitch/loudness, so they are never sent.
    """
    return {
        "type": "config",
        "data": {
            "model": _TTS_MODEL,
            "target_language_code": _lang_code(lang),
            "speaker": speaker,
            "pace": 1.0,
            "temperature": 0.6,
            "enable_preprocessing": True,
            "output_audio_codec": "mulaw",
            "speech_sample_rate": 8000,
            "min_buffer_size": 50,
            "max_chunk_length": 250,
        },
    }


def _ulaw_to_pcm16(ulaw: bytes) -> bytes:
    """Decode G.711 µ-law bytes → PCM16 LE 8kHz (matches CPython audioop.ulaw2lin).

    Sarvam v3 emits µ-law 8k on the wire; the worker's send_audio expects PCM16 8k
    (it µ-law-encodes itself). Decoding here keeps the existing worker contract and
    avoids a double-encode while still removing the old 24k→8k resample.
    """
    import numpy as np

    u = np.frombuffer(ulaw, dtype=np.uint8).astype(np.int32)
    u_comp = (~u) & 0xFF
    sign = (u_comp >> 7) & 1
    exp = (u_comp >> 4) & 0x7
    mant = u_comp & 0xF
    lin14 = (((mant << 1) | 0x21) << exp) - 33
    lin16 = lin14 * 4
    out = np.where(sign == 1, -lin16, lin16)
    return np.clip(out, -32768, 32767).astype("<i2").tobytes()


class _StreamTruncated(RuntimeError):
    """Raised when the Sarvam WS closes/errors BEFORE a real completion event.

    Carries the count of audio chunks already produced so callers can decide
    whether to recover the remainder via REST. The presence of this exception
    type (vs a normal return) is how truncation is distinguished from success.
    """

    def __init__(self, msg: str, chunks_produced: int = 0) -> None:
        super().__init__(msg)
        self.chunks_produced = chunks_produced


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

        Protocol (bulbul:v3 streaming):
          1. open WS to wss://api.sarvam.ai/text-to-speech/ws  (NO query params)
             auth header: API-Subscription-Key: <SARVAM_API_KEY>
          2. send config FIRST — model bulbul:v3 IN config (NOT url), speaker priya,
             output_audio_codec mulaw, speech_sample_rate 8000.
             bulbul:v3 REJECTS pitch/loudness — never send them.
          3. send {"type":"text",...} then {"type":"flush"}.
          4. receive µ-law 8k audio chunks; a completion event ends the stream.

        Yields:
            bytes: PCM16 LE 8kHz. Sarvam emits µ-law 8k on the wire; we decode it to
            PCM16 here so the worker's send_audio (which µ-law-encodes) sees the same
            PCM16 8k contract as the batch path — NO double-encode, NO 24k→8k resample.

        Raises:
            ValueError: if API key missing.
            _StreamTruncated: if the WS closes/errors BEFORE a real completion event —
                the sentence was cut short; caller MUST recover the remainder (REST).
            Exception: on other WS failure — caller falls back to batch synthesize().
        """
        if not self._api_key:
            raise ValueError("SARVAM_API_KEY not configured")
        try:
            import websockets  # type: ignore
        except ImportError as exc:  # pragma: no cover
            raise RuntimeError("websockets package not installed; streaming TTS unavailable") from exc

        # bulbul:v3 speakers; default to priya for unknown ids.
        speaker = voice_id if voice_id in _VALID_SPEAKERS else _DEFAULT_SPEAKER

        # URL is plain (no query params, no path model) — model goes in config frame.
        url = _WS_URL
        headers = {"API-Subscription-Key": self._api_key}
        config = _build_stream_config(speaker, lang)

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

                completed = False
                produced = 0
                try:
                    async for raw in ws:
                        if isinstance(raw, bytes):
                            # Raw binary frame: Sarvam v3 µ-law 8k → decode to PCM16 8k.
                            if raw:
                                produced += 1
                                yield _ulaw_to_pcm16(raw)
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
                                produced += 1
                                yield _ulaw_to_pcm16(base64.b64decode(audio_b64))
                        elif mtype in _COMPLETION_TYPES:
                            completed = True
                            break
                        elif mtype == "error":
                            err_data = msg.get("data") or msg.get("message") or msg
                            err_code = (msg.get("data") or {}).get("code") if isinstance(msg.get("data"), dict) else None
                            log.error(
                                "sarvam_ws_error_frame type=error code=%s body=%r", err_code, err_data
                            )
                            raise RuntimeError(f"sarvam_stream_error code={err_code}: {err_data}")
                except websockets.exceptions.ConnectionClosed as exc:
                    # THE BUG FIX: a close BEFORE a real completion event means the
                    # sentence was TRUNCATED. Do NOT swallow it as a normal end — raise
                    # so the caller recovers the un-spoken remainder (REST fallback).
                    if not completed:
                        log.warning("sarvam_ws_closed_before_completion produced=%d: %r", produced, exc)
                        raise _StreamTruncated(
                            f"closed before completion (produced={produced})", produced
                        ) from exc
                    log.debug("sarvam_ws_connection_closed_after_completion")
                if not completed:
                    # Iterator drained with no close and no completion frame → truncated.
                    raise _StreamTruncated(
                        f"stream ended without completion (produced={produced})", produced
                    )
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


# Sarvam WS 408 idle-timeout fires at ~30s.
# Per Sarvam protocol docs, the correct keepalive is an application-level
# {"type":"ping"} JSON message (NOT just a WS-level ping frame, which Sarvam ignores).
# Interval must be well under 30s — use 8s so we send 3–4 pings before the timeout.
_KEEPALIVE_INTERVAL_S = float(os.getenv("SARVAM_WS_KEEPALIVE_INTERVAL", "8"))


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

        speaker = voice_id if voice_id in _VALID_SPEAKERS else _DEFAULT_SPEAKER
        headers = {"API-Subscription-Key": self._engine._api_key}
        config = _build_stream_config(speaker, lang)
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
        """Send application-level {"type":"ping"} every _KEEPALIVE_INTERVAL_S to prevent 408.

        Sarvam ignores WS-level ping frames; the correct keepalive per their protocol
        is a JSON {"type":"ping"} message sent over the data channel.
        Interval is 8s (well below the 30s idle-timeout) so 3–4 pings fire before cutoff.
        """
        try:
            while True:
                await asyncio.sleep(_KEEPALIVE_INTERVAL_S)
                if self._ws is None:
                    return
                try:
                    await self._ws.send(json.dumps({"type": "ping"}))
                    log.debug("sarvam_ws_keepalive_app_ping sent")
                except Exception as exc:
                    log.warning("sarvam_ws_keepalive_app_ping failed: %r", exc)
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
                completed = False
                produced = 0
                try:
                    import websockets  # type: ignore
                    async for raw in ws:
                        if isinstance(raw, bytes):
                            if raw:
                                produced += 1
                                yield _ulaw_to_pcm16(raw)
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
                                produced += 1
                                yield _ulaw_to_pcm16(base64.b64decode(audio_b64))
                        elif mtype in _COMPLETION_TYPES:
                            completed = True
                            break
                        elif mtype == "error":
                            err_data = msg.get("data") or msg.get("message") or msg
                            err_code = (
                                (msg.get("data") or {}).get("code")
                                if isinstance(msg.get("data"), dict) else None
                            )
                            raise RuntimeError(f"sarvam_stream_error code={err_code}: {err_data}")
                except websockets.exceptions.ConnectionClosed as exc:
                    # Mark WS dead so next utterance reconnects, then decide:
                    self._ws = None
                    self._ws_cm = None
                    if self._ping_task:
                        self._ping_task.cancel()
                        self._ping_task = None
                    if not completed:
                        # TRUNCATION: closed before a real completion event. Raise so the
                        # endpoint recovers the remainder via REST — never silent-drop.
                        log.warning("sarvam_ws_session_closed_before_completion produced=%d: %r", produced, exc)
                        raise _StreamTruncated(
                            f"session closed before completion (produced={produced})", produced
                        ) from exc
                    log.debug("sarvam_ws_session_closed_after_completion")
                if not completed:
                    raise _StreamTruncated(
                        f"session stream ended without completion (produced={produced})", produced
                    )
                return  # success — full sentence streamed to completion
            except _StreamTruncated:
                # Truncation is a real error to surface (endpoint does REST remainder).
                # It is NOT a transient connect failure, so do not burn the retry on it.
                raise
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
