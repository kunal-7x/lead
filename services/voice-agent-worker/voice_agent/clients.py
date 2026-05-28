from __future__ import annotations

import asyncio
import json
import logging
import os
from typing import AsyncIterator, Protocol

import httpx

from voice_agent.models import STTResult, BrainOutput, TTSResult, SessionContext

_STT_URL = os.getenv("STT_ROUTER_URL", "http://stt-router:8110")
_LLM_URL = os.getenv("LLM_ROUTER_URL", "http://llm-router:8111")
_GUARDRAIL_URL = os.getenv("GUARDRAIL_URL", "http://guardrail:8112")
_TTS_URL = os.getenv("TTS_ROUTER_URL", "http://tts-router:8113")

logger = logging.getLogger(__name__)


class STTClient(Protocol):
    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult: ...


class LLMClient(Protocol):
    async def generate(self, ctx: SessionContext, user_turn: str,
                       dialog_history: list[dict],
                       collected_slots: dict | None = None) -> BrainOutput: ...


class GuardrailClient(Protocol):
    async def check(self, brain: BrainOutput, kb_chunks: list[str],
                    user_turn: str) -> BrainOutput: ...


class TTSClient(Protocol):
    async def synthesize(self, text: str, lang: str, voice_id: str,
                         tenant_id: str, session_id: str,
                         tts_premium: bool) -> TTSResult: ...

    def synthesize_stream(self, text: str, lang: str,
                          voice_id: str) -> AsyncIterator[bytes]: ...


# ── HTTP implementations ──────────────────────────────────────────────────────

class HttpSTTClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _STT_URL
        self._client = httpx.AsyncClient(timeout=10.0)
        # WS base: convert http(s) to ws(s)
        self._ws_base = self._base_url.replace("https://", "wss://").replace("http://", "ws://")

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        """Batch STT — POST full WAV, blocks until transcript ready."""
        resp = await self._client.post(
            f"{self._base_url}/v1/stt/batch",
            files={"file": ("audio.raw", audio, "application/octet-stream")},
            data={"lang": lang, "session_id": session_id, "tenant_id": ""},
        )
        resp.raise_for_status()
        return STTResult(**resp.json())

    async def stream_transcribe(
        self,
        audio_queue: asyncio.Queue,  # Queue[bytes | None] — None is sentinel
        lang: str,
        session_id: str,
        tenant_id: str = "",
    ) -> STTResult:
        """Streaming STT via WebSocket — NEW protocol (lang as query param).

        Uses the NEW /v1/stt/stream protocol introduced for Sarvam streaming:
          CONNECT: ws://stt-router:8110/v1/stt/stream?lang=hi-en&session_id=X
          SEND:    raw binary PCM16 8kHz frames (320 bytes = 20ms each)
                   Sentinel: empty binary frame b"" = end-of-utterance
          RECEIVE: {"type":"interim",...} | {"type":"final",...} | {"type":"error",...}

        The lang query param is REQUIRED to activate the Sarvam streaming engine path
        in the STT router. Without it the router falls back to the legacy protocol
        which transcribes each 20ms frame individually via batch STT, returning
        garbage short tokens (e.g. 'Yes.') for every utterance.

        Pulls PCM16 8kHz frames from `audio_queue` (None = end-of-speech), streams
        them, sends 0-byte sentinel, and returns the final transcript.
        Falls back to empty STTResult on any WS error (caller then uses batch path).
        """
        try:
            import websockets  # type: ignore
        except ImportError:
            raise RuntimeError("websockets package not installed; streaming STT unavailable")

        # NEW protocol: lang + session_id as query params (activates Sarvam streaming engine)
        ws_url = (
            f"{self._ws_base}/v1/stt/stream"
            f"?lang={lang}&session_id={session_id}"
        )
        best_text = ""
        best_conf = 0.0
        engine_used = ""

        try:
            async with websockets.connect(ws_url, open_timeout=3, close_timeout=2) as ws:
                # NEW protocol: NO JSON header — go straight to binary frames.

                async def _send_frames():
                    while True:
                        chunk = await audio_queue.get()
                        if chunk is None:
                            break
                        await ws.send(chunk)  # binary PCM16 frame
                    # End of speech — send 0-byte sentinel (NEW protocol)
                    try:
                        await ws.send(b"")
                    except Exception:  # noqa: BLE001
                        pass

                send_task = asyncio.create_task(_send_frames())

                # Collect interim + final results.
                # NEW protocol: {"type":"interim"|"final"|"error"} — no is_final field.
                try:
                    async for raw in ws:
                        try:
                            msg = json.loads(raw)
                        except (ValueError, TypeError):
                            continue
                        mtype = msg.get("type", "")
                        if mtype == "error":
                            err_msg = msg.get("message", "stt_stream_error")
                            if err_msg == "streaming_disabled":
                                # STT_STREAMING_ENGINE not set on server — fall back to batch
                                raise RuntimeError("streaming_disabled")
                            raise RuntimeError(err_msg)
                        text = msg.get("text", "")
                        conf = float(msg.get("confidence", 0.0))
                        eng = msg.get("engine_used", "")
                        if mtype == "final":
                            if text:
                                best_text = text
                                best_conf = conf
                                engine_used = eng
                            break  # final received — done
                        elif mtype == "interim":
                            # Track best interim in case we never get a final
                            if text and conf >= best_conf:
                                best_text = text
                                best_conf = conf
                                if eng:
                                    engine_used = eng
                except RuntimeError:
                    raise  # propagate so caller falls back to batch
                except Exception:  # noqa: BLE001
                    pass
                finally:
                    send_task.cancel()
                    try:
                        await send_task
                    except (asyncio.CancelledError, Exception):
                        pass

        except Exception as exc:  # noqa: BLE001
            logger.warning("Streaming STT WS error: %r — will fall back to batch", exc)
            raise  # let caller fall back

        return STTResult(
            text=best_text,
            confidence=best_conf,
            engine_used=engine_used,
            is_final=True,
        )

    async def aclose(self) -> None:
        await self._client.aclose()


class HttpLLMClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _LLM_URL
        self._client = httpx.AsyncClient(timeout=20.0)

    def _build_payload(self, ctx: SessionContext, user_turn: str,
                       dialog_history: list[dict],
                       collected_slots: dict | None = None) -> dict:
        payload: dict = {
            "user_turn": user_turn,
            "lang": ctx.lang,
            "tenant_id": ctx.tenant_id,
            "session_id": ctx.session_id,
            "project_id": ctx.project_id,
            "system_prompt_version": ctx.system_prompt_version,
            "dialog_history": dialog_history,
        }
        if collected_slots:
            payload["collected_slots"] = collected_slots
        return payload

    async def generate(self, ctx: SessionContext, user_turn: str,
                       dialog_history: list[dict],
                       collected_slots: dict | None = None) -> BrainOutput:
        """Batch LLM — waits for the full reply before returning."""
        payload = self._build_payload(ctx, user_turn, dialog_history, collected_slots)
        resp = await self._client.post(f"{self._base_url}/v1/llm/generate", json=payload)
        resp.raise_for_status()
        body = resp.json()
        return BrainOutput(**body["brain"])

    async def generate_stream_text(
        self,
        ctx: SessionContext,
        user_turn: str,
        dialog_history: list[dict],
        collected_slots: dict | None = None,
    ) -> AsyncIterator[tuple[str, None]]:
        """Plain-text streaming LLM via SSE — ~0.8s first-token, no JSON mode.

        Yields (token_text, None) for each token.
        When the stream ends (done=true) the iterator returns.

        CONTRACT: POST /v1/llm/generate/stream_text — same body as /v1/llm/generate.
        Each SSE event: data: {"token":"...","done":false}
        Final event:    data: {"token":"","done":true}
        """
        payload = self._build_payload(ctx, user_turn, dialog_history, collected_slots)
        async with self._client.stream(
            "POST",
            f"{self._base_url}/v1/llm/generate/stream_text",
            json=payload,
            timeout=30.0,
        ) as resp:
            resp.raise_for_status()
            async for line in resp.aiter_lines():
                if not line.startswith("data:"):
                    continue
                raw = line[5:].strip()
                if not raw:
                    continue
                try:
                    msg = json.loads(raw)
                except (ValueError, TypeError):
                    continue

                token = msg.get("token", "")
                done = msg.get("done", False)

                if done:
                    return
                if token:
                    yield (token, None)

    async def generate_stream(
        self,
        ctx: SessionContext,
        user_turn: str,
        dialog_history: list[dict],
    ) -> AsyncIterator[tuple[str, BrainOutput | None]]:
        """Streaming LLM via SSE.

        Yields (token_text, None) for each incremental token.
        When the stream ends yields ("", brain_output) with the final BrainOutput.

        On any HTTP/SSE error, raises so the caller can fall back to `generate`.

        CONTRACT: POST /v1/llm/generate/stream — same body as /v1/llm/generate.
        Each SSE event: data: {"token":"...","done":false}
        Final event:    data: {"token":"","done":true,"summary":"...","next_action":"...","brain":{...}}
        """
        payload = self._build_payload(ctx, user_turn, dialog_history)
        async with self._client.stream(
            "POST",
            f"{self._base_url}/v1/llm/generate/stream",
            json=payload,
            timeout=30.0,
        ) as resp:
            resp.raise_for_status()
            accumulated = ""
            summary = ""
            next_action = "qualify"
            brain_out: BrainOutput | None = None

            async for line in resp.aiter_lines():
                if not line.startswith("data:"):
                    continue
                raw = line[5:].strip()
                if not raw:
                    continue
                try:
                    msg = json.loads(raw)
                except (ValueError, TypeError):
                    continue

                token = msg.get("token", "")
                done = msg.get("done", False)

                if done:
                    # Final event — build BrainOutput
                    if "brain" in msg and isinstance(msg["brain"], dict):
                        brain_out = BrainOutput(**msg["brain"])
                    else:
                        # Construct from accumulated tokens + metadata
                        brain_out = BrainOutput(
                            reply=accumulated.strip(),
                            next_action=msg.get("next_action", next_action),
                            summary=msg.get("summary", summary),
                        )
                    yield ("", brain_out)
                    return
                else:
                    accumulated += token
                    yield (token, None)

            # Stream ended without a done=true event — build from what we have
            if brain_out is None:
                brain_out = BrainOutput(
                    reply=accumulated.strip(),
                    next_action=next_action,
                    summary=summary,
                )
            yield ("", brain_out)

    async def aclose(self) -> None:
        await self._client.aclose()


class HttpGuardrailClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _GUARDRAIL_URL
        self._client = httpx.AsyncClient(timeout=5.0)

    async def check(self, brain: BrainOutput, kb_chunks: list[str],
                    user_turn: str) -> BrainOutput:
        payload = {
            "brain": brain.model_dump(),
            "kb_chunks": kb_chunks,
            "user_turn": user_turn,
        }
        resp = await self._client.post(f"{self._base_url}/v1/guardrail/check", json=payload)
        resp.raise_for_status()
        body = resp.json()
        return BrainOutput(**body["brain"])

    async def aclose(self) -> None:
        await self._client.aclose()


class HttpTTSClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _TTS_URL
        self._client = httpx.AsyncClient(timeout=10.0)
        self._ws_base = self._base_url.replace("https://", "wss://").replace("http://", "ws://")
        # Per-session Sarvam streaming WS (reused across turns; closed on hangup).
        self._stream_ws = None  # type: ignore[assignment]
        self._stream_lock = asyncio.Lock()

    async def synthesize(self, text: str, lang: str, voice_id: str,
                         tenant_id: str, session_id: str,
                         tts_premium: bool = False) -> TTSResult:
        payload = {
            "text": text, "lang": lang, "voice_id": voice_id,
            "tenant_id": tenant_id, "session_id": session_id,
            "tts_premium": tts_premium,
        }
        resp = await self._client.post(f"{self._base_url}/v1/tts/synthesize", json=payload)
        resp.raise_for_status()
        return TTSResult(
            audio=resp.content,
            tier_used=resp.headers.get("X-Tier-Used", ""),
            cache_hit=resp.headers.get("X-Cache-Hit", "false") == "true",
        )

    async def _ensure_stream_ws(self):
        """Open (or reuse) the per-session Sarvam streaming WS.

        Reconnects if the existing connection is closed. One WS per session is
        reused across turns; closed via aclose() on hangup.
        """
        import websockets  # type: ignore

        ws = self._stream_ws
        if ws is not None and getattr(ws, "close_code", None) is None:
            try:
                # websockets >=11 exposes .state; treat OPEN as reusable.
                if getattr(ws, "state", None) is None or str(ws.state).endswith("OPEN"):
                    return ws
            except Exception:  # noqa: BLE001
                pass
        ws_url = f"{self._ws_base}/v1/tts/sarvam/stream"
        self._stream_ws = await websockets.connect(ws_url, open_timeout=3, close_timeout=2)
        return self._stream_ws

    async def synthesize_stream(self, text: str, lang: str,
                                voice_id: str) -> AsyncIterator[bytes]:
        """Stream PCM16 8kHz audio chunks for one utterance via the Sarvam WS.

        Yields raw PCM16 LE 8kHz bytes AS THEY ARRIVE (first chunk ~0.3s) so the
        worker can feed them to send_audio immediately. Reuses a single WS per
        session. On 'streaming_disabled' (TTS_STREAMING_WS=false on the router) or
        any WS error, raises so the caller falls back to the batch synthesize() path.

        Wire protocol with /v1/tts/sarvam/stream:
          send {"text","voice_id","lang"} → recv binary PCM16 chunks → recv
          {"type":"done",...} terminates this utterance (WS stays open for reuse).
        """
        async with self._stream_lock:
            ws = await self._ensure_stream_ws()
            await ws.send(json.dumps({"text": text, "voice_id": voice_id, "lang": lang}))
            completed = False
            try:
                async for frame in ws:
                    if isinstance(frame, bytes):
                        if frame:
                            yield frame
                        continue
                    try:
                        msg = json.loads(frame)
                    except (ValueError, TypeError):
                        continue
                    mtype = msg.get("type", "")
                    if mtype == "done":
                        completed = True
                        logger.info(
                            "[diag] phase=tts_stream first_chunk_ms=%s total_chunks=%s engine=%s",
                            msg.get("first_chunk_ms"), msg.get("total_chunks"),
                            msg.get("engine"),
                        )
                        return
                    if mtype == "error":
                        err = msg.get("message", "tts_stream_error")
                        # streaming_disabled = flag off; reset WS so we don't reuse it.
                        await self._close_stream_ws()
                        raise RuntimeError(err)
            finally:
                if not completed:
                    # Router WS dropped mid-relay BEFORE the done frame => the sentence
                    # was truncated. Reset the WS and signal the caller (agent.py) so it
                    # recovers the full sentence via batch REST — never silent-drop.
                    await self._close_stream_ws()
            # Stream ended without a 'done' frame: truncation. Raise so _synth_and_play_stream
            # returns False and the batch REST path re-speaks the full sentence.
            raise RuntimeError("tts_stream_truncated_no_done")

    async def _close_stream_ws(self) -> None:
        ws = self._stream_ws
        self._stream_ws = None
        if ws is not None:
            try:
                await ws.close()
            except Exception:  # noqa: BLE001
                pass

    async def aclose(self) -> None:
        await self._close_stream_ws()
        await self._client.aclose()
