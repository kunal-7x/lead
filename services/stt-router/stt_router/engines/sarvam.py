from __future__ import annotations

import asyncio
import base64
import json
import os
import time
from typing import AsyncIterator

import httpx

from stt_router.engines.base import STTEngine
from stt_router.models import STTResult

_SARVAM_API_KEY = os.getenv("SARVAM_API_KEY", "")
# translate endpoint: returns transcript + translation; benchmarked faster than
# the plain /speech-to-text endpoint on Sarvam's infra (1.5s vs 2.1s warm).
# Switch to plain endpoint via env: SARVAM_USE_TRANSLATE=0
_USE_TRANSLATE = os.getenv("SARVAM_USE_TRANSLATE", "1") != "0"
_TRANSLATE_URL = "https://api.sarvam.ai/speech-to-text-translate"
_STT_URL = "https://api.sarvam.ai/speech-to-text"
_BATCH_URL = _TRANSLATE_URL if _USE_TRANSLATE else _STT_URL
# Per-request httpx timeout. Keep below router timeout (STT_SARVAM_TIMEOUT_S, default 20s)
# so httpx raises ReadTimeout BEFORE asyncio.wait_for cancels, giving the retry path
# a chance to run on broken connections. Set to 8s (p99 of Sarvam warm latency ~5s);
# retry adds another ≤8s, total ≤16s well within the 20s router window.
_TIMEOUT_S = float(os.getenv("SARVAM_REQUEST_TIMEOUT_S", "8.0"))

# HTTP/2 multiplexing gives ~3x latency reduction vs HTTP/1.1 by reusing the
# TLS connection across requests (measured: 4.5s → 1.6s warm avg).
# Requires httpx[http2] (h2 package). Falls back gracefully if unavailable.
try:
    import h2  # noqa: F401 — just checking availability
    _HTTP2 = True
except ImportError:
    _HTTP2 = False


class SarvamEngine(STTEngine):
    """Sarvam Saarika v2 — best Hinglish/Hindi accuracy, API-only (no GPU).

    Production: set SARVAM_API_KEY env var.
    Audio: upsample 8kHz→16kHz before sending (Sarvam requires 16kHz PCM).
    Endpoint: speech-to-text-translate (faster than plain STT, see benchmark).
    HTTP/2: enabled when h2 package is installed (httpx[http2]) — 3x latency win.
    Streaming: wss://api.sarvam.ai/v1/realtime/stream — deferred (see HUMAN_TASKS.md).
    """

    name = "sarvam"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT_S) -> None:
        self._api_key = api_key or _SARVAM_API_KEY
        self._timeout = timeout
        # Single persistent client with HTTP/2 for connection reuse across calls.
        # http2=True requires 'h2' package; falls back to HTTP/1.1 if unavailable.
        self._client = httpx.AsyncClient(
            timeout=timeout,
            http2=_HTTP2,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        t0 = time.time()
        # Upsample 8kHz → 16kHz by linear interpolation (simple 2x)
        pcm = _upsample_8k_to_16k(audio)

        headers = {"API-Subscription-Key": self._api_key}
        files = {"file": ("audio.wav", _wrap_pcm_wav(pcm, sample_rate=16000), "audio/wav")}
        data = {"language_code": _lang_code(lang), "model": "saaras:v2.5"}

        # One retry on connection errors (HTTP/2 GOAWAY / server-side idle close).
        # On connection failure: close the stale client, open a fresh one, retry once.
        for attempt in range(2):
            try:
                resp = await self._client.post(_BATCH_URL, headers=headers, files=files, data=data)
                break
            except (httpx.ReadTimeout, httpx.RemoteProtocolError, httpx.ConnectError) as exc:
                if attempt == 0:
                    # Recycle the client to clear any broken HTTP/2 connection state
                    try:
                        await self._client.aclose()
                    except Exception:
                        pass
                    self._client = httpx.AsyncClient(
                        timeout=self._timeout,
                        http2=_HTTP2,
                        limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
                    )
                else:
                    raise  # propagate on second failure

        resp.raise_for_status()
        body = resp.json()

        text = body.get("transcript", "")
        confidence = float(body.get("confidence", 0.9))
        latency_ms = int((time.time() - t0) * 1000)
        return self._make_result(text, confidence, lang, t0, latency_ms)

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.sarvam.ai/v1/health",
                headers={"API-Subscription-Key": self._api_key},
                timeout=3.0,
            )
            return resp.status_code < 500
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()


_STREAMING_WS_URL = "wss://api.sarvam.ai/speech-to-text/ws"
# µ-law to linear PCM lookup table (ITU-T G.711)
_ULAW_TO_LINEAR: list[int] | None = None


def _get_ulaw_table() -> list[int]:
    """Build µ-law decode table once (cached)."""
    global _ULAW_TO_LINEAR
    if _ULAW_TO_LINEAR is not None:
        return _ULAW_TO_LINEAR
    table = []
    for u in range(256):
        u_val = ~u & 0xFF
        sign = u_val & 0x80
        exp = (u_val >> 4) & 0x07
        mantissa = u_val & 0x0F
        linear = ((mantissa << 3) + 0x84) << exp
        linear -= 0x84
        if sign:
            linear = -linear
        table.append(linear)
    _ULAW_TO_LINEAR = table
    return table


def ulaw_to_pcm16(ulaw_bytes: bytes) -> bytes:
    """Decode G.711 µ-law bytes to signed 16-bit little-endian PCM.

    This is the inverse of the G.711 µ-law encoding used by telephony.
    Input: raw µ-law bytes (1 byte per sample at 8kHz)
    Output: PCM16 LE bytes (2 bytes per sample at 8kHz)
    """
    import struct
    table = _get_ulaw_table()
    return struct.pack(f"<{len(ulaw_bytes)}h", *(table[b] for b in ulaw_bytes))


class SarvamStreamingEngine:
    """Sarvam Saaras V3 real-time streaming STT via WebSocket.

    Protocol (VERIFIED LIVE against api.sarvam.ai 2026-05-26 from BLR droplet):
      URL: wss://api.sarvam.ai/speech-to-text/ws
        query: ?language-code=hi-IN&model=saaras:v3&mode=transcribe
               &sample_rate=8000&input_audio_codec=pcm_s16le&vad_signals=true
        MODEL: saaras:v3 is REQUIRED for realtime. saaras:v2.5 is REJECTED with
               WS close 4000 "Invalid model 'saaras:v2.5'. Only 'saarika:v2.5' or
               'saaras:v3' are supported." (batch translate still uses v2.5.)
      Auth: Api-Subscription-Key header.
      Audio send: JSON {"audio": {"data": "<base64_wav>", "sample_rate": "8000", "encoding": "audio/wav"}}
        - Each message wraps PCM16 8kHz audio in a WAV container before base64-encoding.
        - Chunked sends (e.g. 100-500ms) are accepted; server VAD endpoints on flush.
      Flush: JSON {"type": "flush"} — forces immediate endpointing. REQUIRED: without
             it, server VAD did not reliably fire END_SPEECH within the window.
      Receive:
        - {"type": "events", "data": {"signal_type": "START_SPEECH"/"END_SPEECH", "occured_at": ts}}
        - {"type": "data", "data": {"transcript": "...", "language_probability": null|float,
            "language_code": "hi-IN", "metrics": {"audio_duration": .., "processing_latency": ..}}}
        - WS close 4000 on protocol/model errors.
      NOTE: language_probability is present-but-null on v3 → coalesce to 0.9.

    Measured latency (droplet, 237ms RTT): END_SPEECH(server)->final ~258ms,
    speech_end(client)->final ~298ms, server processing_latency ~238ms.
    vs batch saaras translate ~3500ms — ~3.2s saved on the critical path.

    µ-law input: worker sends raw G.711 µ-law 8kHz; engine transcodes to PCM16 internally
    before wrapping in WAV and base64-encoding. Worker never does codec conversion.

    Usage:
        engine = SarvamStreamingEngine()
        async for event in engine.stream_utterance(ulaw_8khz_audio, lang="hi-en"):
            if event["type"] == "interim":
                print("interim:", event["text"])
            elif event["type"] == "final":
                print("final:", event["text"], "latency_ms:", event["latency_ms"])
    """

    name = "sarvam_streaming"

    def __init__(self, api_key: str = "") -> None:
        self._api_key = api_key or _SARVAM_API_KEY
        # Reconnect backoff state per instance
        self._backoff_s: float = 0.5

    async def stream_utterance(
        self,
        audio_frames: bytes | AsyncIterator[bytes],
        lang: str = "hi-en",
        session_id: str = "",
        chunk_size: int = 4000,  # 500ms at 8kHz PCM16; larger = fewer round-trips
        audio_format: str = "pcm16",  # "pcm16" or "ulaw"
    ) -> AsyncIterator[dict]:
        """Stream an utterance to Sarvam and yield interim + final events.

        Args:
            audio_frames: Raw PCM16 8kHz bytes (bulk or AsyncIterator of chunks),
                          OR raw µ-law 8kHz bytes when audio_format="ulaw".
                          Worker sends raw µ-law from Vobiz; engine transcodes.
            lang: Language code ("hi-en", "hi", "en").
            session_id: For logging.
            chunk_size: Bytes per WS send. 4000 = 500ms at 8kHz PCM16.
            audio_format: "pcm16" (default) or "ulaw" (G.711 µ-law 8kHz from telephony).

        Yields:
            {"type": "interim", "text": "...", "ts": float}   — partial results (VAD start)
            {"type": "final", "text": "...", "confidence": float, "latency_ms": int, "ts": float}
            {"type": "error", "message": "..."}
        """
        import websockets

        lang_code = _lang_code(lang)
        params = (
            f"?language-code={lang_code}"
            "&model=saaras:v3"
            "&mode=transcribe"
            "&sample_rate=8000"
            "&input_audio_codec=pcm_s16le"
            "&vad_signals=true"
        )
        url = _STREAMING_WS_URL + params
        extra_headers = {"Api-Subscription-Key": self._api_key}

        t0 = time.time()
        t_end_speech: float | None = None

        # Collect all audio if given as bytes (bulk mode)
        if isinstance(audio_frames, bytes):
            audio_bytes = audio_frames
        else:
            # AsyncIterator: collect all frames
            parts = []
            async for frame in audio_frames:
                parts.append(frame)
            audio_bytes = b"".join(parts)

        # Transcode µ-law → PCM16 if needed
        if audio_format == "ulaw":
            audio_bytes = ulaw_to_pcm16(audio_bytes)

        try:
            async with websockets.connect(url, additional_headers=extra_headers) as ws:
                # Send audio in chunks
                chunks = [
                    audio_bytes[i: i + chunk_size]
                    for i in range(0, len(audio_bytes), chunk_size)
                ] if audio_bytes else [b""]

                for chunk in chunks:
                    if not chunk:
                        continue
                    wav_chunk = _wrap_pcm_wav(chunk, sample_rate=8000)
                    msg = json.dumps({
                        "audio": {
                            "data": base64.b64encode(wav_chunk).decode("ascii"),
                            "sample_rate": "8000",
                            "encoding": "audio/wav",
                        }
                    })
                    await ws.send(msg)

                # Flush to force VAD endpointing
                await ws.send(json.dumps({"type": "flush"}))
                t_flush = time.time()

                # Collect responses until final transcript or timeout
                deadline = time.time() + 10.0
                while time.time() < deadline:
                    remaining = deadline - time.time()
                    try:
                        raw = await asyncio.wait_for(ws.recv(), timeout=max(remaining, 0.1))
                    except asyncio.TimeoutError:
                        break

                    msg_obj = json.loads(raw)
                    mtype = msg_obj.get("type")

                    if mtype == "events":
                        sig = msg_obj.get("data", {}).get("signal_type", "")
                        if sig == "START_SPEECH":
                            yield {"type": "interim", "text": "", "ts": time.time()}
                        elif sig == "END_SPEECH":
                            t_end_speech = time.time()

                    elif mtype == "data":
                        data = msg_obj.get("data", {})
                        text = data.get("transcript", "")
                        t_final = time.time()
                        latency_ms = int(
                            (t_final - (t_end_speech or t_flush)) * 1000
                        )
                        # language_probability is present-but-null in saaras:v3
                        # responses, so .get(..., default) won't catch it.
                        conf = data.get("language_probability")
                        conf = float(conf) if conf is not None else 0.9
                        yield {
                            "type": "final",
                            "text": text,
                            "confidence": conf,
                            "latency_ms": latency_ms,
                            "ts": t_final,
                            "engine_used": self.name,
                        }
                        return  # Final transcript received, done

                    elif mtype == "error":
                        err = msg_obj.get("data", {}).get("message", str(msg_obj))
                        yield {"type": "error", "message": err}
                        return

        except Exception as exc:
            yield {"type": "error", "message": str(exc)}

    async def stream_live(
        self,
        frame_queue: "asyncio.Queue",
        lang: str = "hi-en",
        session_id: str = "",
        audio_format: str = "pcm16",
        send_chunk_bytes: int = 1600,  # 100ms at 8kHz PCM16 — forward during speech
    ) -> AsyncIterator[dict]:
        """TRUE during-speech streaming.

        Opens the Sarvam WS immediately and forwards PCM frames to Sarvam AS THEY
        ARRIVE on `frame_queue` (so Sarvam processes audio WHILE the caller is
        still speaking). On the end-of-utterance sentinel (None in the queue) we
        send {"type":"flush"} and return the final — which arrives in ~150-300ms
        because the audio was already streamed during speech.

        Contrast with stream_utterance(), which buffers the whole utterance then
        flushes (pays the full STT round-trip AFTER speech_end → ~3.4s wall-clock
        when you include the real-time pacing of the inbound frames).

        Args:
            frame_queue: asyncio.Queue yielding raw audio frames (bytes). A None
                value is the end-of-utterance sentinel.
            lang: language code.
            session_id: for logging.
            audio_format: "pcm16" (default) or "ulaw" (transcoded per-frame).
            send_chunk_bytes: bytes to accumulate before each WS send (100ms keeps
                round-trips low while still forwarding during speech).

        Yields the same event shapes as stream_utterance().
        """
        import websockets

        lang_code = _lang_code(lang)
        params = (
            f"?language-code={lang_code}"
            "&model=saaras:v3"
            "&mode=transcribe"
            "&sample_rate=8000"
            "&input_audio_codec=pcm_s16le"
            "&vad_signals=true"
        )
        url = _STREAMING_WS_URL + params
        extra_headers = {"Api-Subscription-Key": self._api_key}

        t_first_frame: float | None = None
        t_flush: float | None = None
        t_end_speech: float | None = None
        send_buf = bytearray()

        async def _send_audio_chunk(ws, pcm_chunk: bytes) -> None:
            wav_chunk = _wrap_pcm_wav(pcm_chunk, sample_rate=8000)
            await ws.send(json.dumps({
                "audio": {
                    "data": base64.b64encode(wav_chunk).decode("ascii"),
                    "sample_rate": "8000",
                    "encoding": "audio/wav",
                }
            }))

        try:
            async with websockets.connect(url, additional_headers=extra_headers) as ws:
                # ── Producer: pull frames from queue, forward DURING speech ──────
                async def _pump() -> None:
                    nonlocal t_first_frame, t_flush
                    while True:
                        frame = await frame_queue.get()
                        if frame is None:
                            # End-of-utterance: flush any partial buffer, then flush WS
                            if send_buf:
                                chunk = bytes(send_buf)
                                send_buf.clear()
                                if audio_format == "ulaw":
                                    chunk = ulaw_to_pcm16(chunk)
                                await _send_audio_chunk(ws, chunk)
                            await ws.send(json.dumps({"type": "flush"}))
                            t_flush = time.time()
                            return
                        if t_first_frame is None:
                            t_first_frame = time.time()
                        send_buf.extend(frame)
                        # Forward in ~100ms chunks while speech continues
                        while len(send_buf) >= send_chunk_bytes:
                            chunk = bytes(send_buf[:send_chunk_bytes])
                            del send_buf[:send_chunk_bytes]
                            if audio_format == "ulaw":
                                chunk = ulaw_to_pcm16(chunk)
                            await _send_audio_chunk(ws, chunk)

                pump_task = asyncio.create_task(_pump())

                try:
                    # ── Consumer: read Sarvam events until final/error/timeout ──
                    deadline = time.time() + 30.0  # overall cap incl. speech time
                    while time.time() < deadline:
                        remaining = deadline - time.time()
                        try:
                            raw = await asyncio.wait_for(
                                ws.recv(), timeout=max(remaining, 0.1)
                            )
                        except asyncio.TimeoutError:
                            break

                        msg_obj = json.loads(raw)
                        mtype = msg_obj.get("type")

                        if mtype == "events":
                            sig = msg_obj.get("data", {}).get("signal_type", "")
                            if sig == "START_SPEECH":
                                yield {"type": "interim", "text": "", "ts": time.time()}
                            elif sig == "END_SPEECH":
                                t_end_speech = time.time()

                        elif mtype == "data":
                            data = msg_obj.get("data", {})
                            text = data.get("transcript", "")
                            t_final = time.time()
                            base_ts = t_end_speech or t_flush or t_final
                            latency_ms = int((t_final - base_ts) * 1000)
                            conf = data.get("language_probability")
                            conf = float(conf) if conf is not None else 0.9
                            yield {
                                "type": "final",
                                "text": text,
                                "confidence": conf,
                                "latency_ms": latency_ms,
                                "ts": t_final,
                                "engine_used": self.name,
                            }
                            return

                        elif mtype == "error":
                            err = msg_obj.get("data", {}).get("message", str(msg_obj))
                            yield {"type": "error", "message": err}
                            return
                finally:
                    pump_task.cancel()
                    try:
                        await pump_task
                    except (asyncio.CancelledError, Exception):
                        pass

        except Exception as exc:
            yield {"type": "error", "message": str(exc)}

    async def transcribe_streaming(
        self, audio: bytes, lang: str, session_id: str, audio_format: str = "pcm16"
    ) -> STTResult:
        """Batch-compat wrapper: stream the full audio, return final STTResult.

        Used by STTRouter when STT_STREAMING_ENGINE=sarvam_streaming.
        """
        t0 = time.time()
        final_text = ""
        confidence = 0.9
        latency_ms = 0

        async for event in self.stream_utterance(
            audio, lang, session_id, audio_format=audio_format
        ):
            if event["type"] == "final":
                final_text = event["text"]
                confidence = float(event.get("confidence", 0.9))
                latency_ms = event.get("latency_ms", 0)
                break
            elif event["type"] == "error":
                raise RuntimeError(f"Sarvam streaming error: {event['message']}")

        return STTResult(
            text=final_text,
            is_final=True,
            confidence=confidence,
            language=lang,
            engine_used=self.name,
            latency_ms=latency_ms or int((time.time() - t0) * 1000),
            turn_start_ts=t0,
            turn_end_ts=time.time(),
        )

    async def health_check(self) -> bool:
        """Verify WS connection can be established."""
        if not self._api_key:
            return False
        import websockets
        try:
            url = (
                _STREAMING_WS_URL
                + "?language-code=hi-IN&model=saaras:v3&mode=transcribe&sample_rate=8000"
            )
            async with websockets.connect(
                url,
                additional_headers={"Api-Subscription-Key": self._api_key},
                open_timeout=5.0,
            ):
                return True
        except Exception:
            return False


def _lang_code(lang: str) -> str:
    mapping = {"hi": "hi-IN", "hi-en": "hi-IN", "en": "en-IN"}
    return mapping.get(lang, "hi-IN")


def _upsample_8k_to_16k(pcm8k: bytes) -> bytes:
    """Double sample rate by linear interpolation (8kHz → 16kHz).

    Vectorized numpy implementation: equivalent to the previous pure-Python
    loop but ~10-50x faster on multi-second audio buffers.
    Each input sample S[i] becomes two output samples:
      out[2i]   = S[i]
      out[2i+1] = (S[i] + S[i+1]) // 2   (midpoint interpolation, last repeats)
    """
    import numpy as np
    import struct

    n = len(pcm8k) // 2
    if n == 0:
        return b""
    samples = np.frombuffer(pcm8k, dtype="<i2")  # signed 16-bit little-endian
    # Neighbour for each sample: shift right by 1, last element repeats itself
    nxt = np.empty_like(samples)
    nxt[:-1] = samples[1:]
    nxt[-1] = samples[-1]
    # Interleave: even indices = original, odd indices = midpoint
    out = np.empty(n * 2, dtype=np.int16)
    out[0::2] = samples
    out[1::2] = ((samples.astype(np.int32) + nxt.astype(np.int32)) // 2).astype(np.int16)
    return out.tobytes()


def _wrap_pcm_wav(pcm: bytes, sample_rate: int = 16000, channels: int = 1, bits: int = 16) -> bytes:
    """Wrap raw L16 PCM bytes in a minimal WAV container."""
    import struct
    data_size = len(pcm)
    header = struct.pack(
        "<4sI4s4sIHHIIHH4sI",
        b"RIFF", 36 + data_size, b"WAVE",
        b"fmt ", 16, 1, channels, sample_rate,
        sample_rate * channels * bits // 8,
        channels * bits // 8, bits,
        b"data", data_size,
    )
    return header + pcm
