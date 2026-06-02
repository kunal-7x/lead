"""LlmRouterProcessor — real LLM stage for voice-agent-v2.

Replaces EchoLLMProcessor in agent.py.

Contract with llm-router (confirmed from services/llm-router/llm_router/app.py +
router.py — NOT guessed):

  STREAMING SPEECH ENDPOINT
    POST {LLM_ROUTER_URL}/v1/llm/generate/stream_text
    Content-Type: application/json
    Body: LLMRequest (see payload schema below)

    Response: text/event-stream (Server-Sent Events)
      Token events : data: {"token":"<piece>","done":false}\\n\\n
      Final event  : data: {"token":"","done":true}\\n\\n

  STRUCTURED BATCH ENDPOINT (metadata — NOT called by default, see note below)
    POST {LLM_ROUTER_URL}/v1/llm/generate
    Returns JSON: {"brain": <BrainOutput>, "model_used": ..., "latency_ms": ...}

  REQUEST PAYLOAD (LLMRequest schema — all fields):
    Required:
      user_turn          str         — the caller's utterance
      tenant_id          str
      session_id         str
      lang               str         — default "hi-en"
    Optional (sent when non-empty):
      project_id         str
      system_prompt_version  str     — default "v1"
      dialog_history     list[{role, content}]  — prior turns (windowed)
      persona_gender     str         — "male" | "female"
      collected_slots    dict        — budget/location/etc. accumulated so far
      system_prompt_suffix str       — per-turn CSO/RSP directive
      campaign_context   dict        — product/offer/talking points
      kb_chunks          list        — pre-fetched KB context (skip KB re-fetch)

BUGS FIXED vs the old voice-agent-worker scaffold:
  1. Turn-fragmentation: only FINALIZED TranscriptionFrame triggers a call
     (frame.finalized must be True). Interim/partial frames are passed through
     unchanged — they NEVER fire the LLM.
  2. Double-call / rate-limit amplifier: this processor makes EXACTLY ONE
     upstream call per finalized turn (stream_text only). No parallel /generate
     unless an explicit structured metadata request is made in future.
  3. Barge-in duplicate: on StartInterruptionFrame the in-flight task is
     cancelled BEFORE the new task could start; the interrupt frame itself is
     forwarded so the pipeline can flush audio. A supersede check in
     process_frame prevents a second task from racing.
  4. Client lifecycle: the httpx.AsyncClient is owned + closed by this processor
     (cleanup()). It is NOT a module-level singleton that leaks.

LLM_ROUTER_URL env var controls the base URL; defaults to http://llm-router:8111
(matches AgentSettings.llm_router_url).
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
from typing import Any

import httpx

logger = logging.getLogger(__name__)

# History window: keep last N messages in dialog context sent to llm-router.
# Matches the old worker default (12). Override via LLM_HISTORY_MAX_MSGS env.
_DEFAULT_HISTORY_MAX = 12


# ---------------------------------------------------------------------------
# Pipecat imports with shim fallback (same pattern as voice-agent-worker)
# ---------------------------------------------------------------------------
try:  # pragma: no cover
    from pipecat.frames.frames import (  # type: ignore
        Frame,
        LLMFullResponseEndFrame,
        LLMFullResponseStartFrame,
        TextFrame,
        TranscriptionFrame,
    )
    from pipecat.processors.frame_processor import (  # type: ignore
        FrameDirection,
        FrameProcessor,
    )

    # pipecat 1.3 renamed UserInterruptionFrame → StartInterruptionFrame;
    # try both names so the code works across patch versions.
    try:
        from pipecat.frames.frames import StartInterruptionFrame  # type: ignore
    except ImportError:
        try:
            from pipecat.frames.frames import UserInterruptionFrame as StartInterruptionFrame  # type: ignore
        except ImportError:
            StartInterruptionFrame = None  # type: ignore[assignment,misc]

except Exception:  # noqa: BLE001
    # Running under pytest without pipecat installed — use minimal shim.
    from voice_agent_v2._pipecat_shim import (  # type: ignore
        Frame,
        FrameDirection,
        FrameProcessor,
        LLMFullResponseEndFrame,
        LLMFullResponseStartFrame,
        StartInterruptionFrame,
        TextFrame,
        TranscriptionFrame,
    )


# ---------------------------------------------------------------------------
# Processor
# ---------------------------------------------------------------------------

class LlmRouterProcessor(FrameProcessor):
    """LLM stage: call llm-router streaming endpoint per finalized transcript.

    Lifecycle:
      1. process_frame receives a finalized TranscriptionFrame.
      2. Any in-flight generation task is cancelled first (supersede / barge-in).
      3. A new asyncio Task is created for _generate().
      4. _generate() POSTs to /v1/llm/generate/stream_text (ONE call).
      5. SSE tokens are pushed downstream as TextFrame instances as they arrive.
      6. LLMFullResponseStartFrame / LLMFullResponseEndFrame bracket the stream.
      7. The user transcript is forwarded UPSTREAM (direction preserved) so the
         pipeline's turn-tracking machinery sees it correctly.

    Barge-in:
      StartInterruptionFrame cancels _gen_task, then is forwarded downstream.

    Cleanup:
      cleanup() cancels any in-flight task and closes the httpx client.
    """

    def __init__(
        self,
        ctx: Any,                               # SessionContext
        settings: Any,                          # AgentSettings
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        super().__init__()
        self._ctx = ctx
        self._settings = settings
        self._base_url = (
            getattr(settings, "llm_router_url", None)
            or os.getenv("LLM_ROUTER_URL", "http://llm-router:8111")
        ).rstrip("/")
        self._history_max = max(
            2,
            int(os.getenv("LLM_HISTORY_MAX_MSGS", str(_DEFAULT_HISTORY_MAX))),
        )
        self._dialog_history: list[dict] = []
        self._collected_slots: dict = {}
        self._client = http_client or httpx.AsyncClient(timeout=30.0)
        self._owns_client = http_client is None
        # In-flight streaming task — cancelled on barge-in or supersede.
        self._gen_task: asyncio.Task | None = None

    # ------------------------------------------------------------------
    # FrameProcessor entrypoint
    # ------------------------------------------------------------------

    async def process_frame(self, frame: Any, direction: Any) -> None:
        await super().process_frame(frame, direction)

        # Barge-in: cancel current generation, forward the interruption frame.
        if StartInterruptionFrame is not None and isinstance(frame, StartInterruptionFrame):
            await self._cancel_inflight()
            await self.push_frame(frame, direction)
            return

        if isinstance(frame, TranscriptionFrame):
            # BUG-FIX 1: only fire on FINALIZED transcription.
            # Pipecat sets frame.finalized = True when the STT engine commits.
            # Interim/partial frames (finalized=False) are passed through unchanged
            # so downstream processors (e.g. turn-tracking) see them, but they do
            # NOT trigger an LLM call.
            is_final = getattr(frame, "finalized", True)  # default True for safety
            if not is_final:
                await self.push_frame(frame, direction)
                return

            user_turn = (getattr(frame, "text", "") or "").strip()
            if not user_turn:
                await self.push_frame(frame, direction)
                return

            # Forward the transcript frame in its original direction so the
            # pipeline's turn-tracking sees it before we start generating.
            await self.push_frame(frame, direction)

            # BUG-FIX 3: supersede any still-running generation before spawning
            # a new one (defensive; barge-in normally cancels first via
            # StartInterruptionFrame, but a rapid double-fire still lands here).
            await self._cancel_inflight()

            # BUG-FIX 2: spawn ONE task — stream_text only, no parallel /generate.
            self._gen_task = asyncio.create_task(self._generate(user_turn))
            return

        # Everything else passes through unchanged.
        await self.push_frame(frame, direction)

    # ------------------------------------------------------------------
    # Generation (ONE streaming call per turn)
    # ------------------------------------------------------------------

    async def _generate(self, user_turn: str) -> None:
        """POST stream_text → push TextFrames downstream as tokens arrive."""
        payload = self._build_payload(user_turn)
        # Append user turn to history AFTER building the payload (llm-router
        # expects dialog_history = prior context, user_turn = current turn).
        self._append_history("user", user_turn)

        reply_parts: list[str] = []
        started = False
        try:
            async with self._client.stream(
                "POST",
                f"{self._base_url}/v1/llm/generate/stream_text",
                json=payload,
                timeout=30.0,
            ) as resp:
                resp.raise_for_status()
                # Signal to TTS that LLM output is starting.
                await self.push_frame(
                    LLMFullResponseStartFrame(), FrameDirection.DOWNSTREAM
                )
                started = True
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
                    if msg.get("done", False):
                        break
                    token = msg.get("token", "")
                    if not token:
                        continue
                    reply_parts.append(token)
                    # Stream token to TTS immediately — TTS starts speaking before
                    # the LLM finishes (pipeline streaming effect).
                    await self.push_frame(
                        TextFrame(text=token), FrameDirection.DOWNSTREAM
                    )
        except asyncio.CancelledError:
            # Clean close: bracket the response if we opened it, then re-raise.
            if started:
                try:
                    await self.push_frame(
                        LLMFullResponseEndFrame(), FrameDirection.DOWNSTREAM
                    )
                except Exception:  # noqa: BLE001
                    pass
            raise
        except Exception as exc:  # noqa: BLE001
            logger.warning(
                "llm-router stream_text failed session=%s err=%r",
                getattr(self._ctx, "session_id", "?"),
                exc,
            )
        finally:
            if started:
                try:
                    await self.push_frame(
                        LLMFullResponseEndFrame(), FrameDirection.DOWNSTREAM
                    )
                except Exception:  # noqa: BLE001
                    pass

        # Accumulate full reply for dialog history.
        reply = "".join(reply_parts).strip()
        if reply:
            self._append_history("assistant", reply)

    # ------------------------------------------------------------------
    # Payload builder
    # ------------------------------------------------------------------

    def _build_payload(self, user_turn: str) -> dict:
        ctx = self._ctx
        payload: dict = {
            "user_turn": user_turn,
            "lang": getattr(ctx, "lang", "hi-en"),
            "tenant_id": getattr(ctx, "tenant_id", ""),
            "session_id": getattr(ctx, "session_id", ""),
            "project_id": getattr(ctx, "project_id", ""),
            "system_prompt_version": getattr(ctx, "system_prompt_version", "v1"),
            "dialog_history": list(self._dialog_history),
            "persona_gender": _gender_for_speaker(
                getattr(ctx, "voice_profile_id", "rahul")
            ),
        }
        if self._collected_slots:
            payload["collected_slots"] = dict(self._collected_slots)
        suffix = getattr(ctx, "system_prompt_suffix", None)
        if callable(suffix):
            suffix = suffix()
        if suffix:
            payload["system_prompt_suffix"] = suffix
        campaign_context = getattr(ctx, "campaign_context", None)
        if campaign_context:
            payload["campaign_context"] = campaign_context
        return payload

    # ------------------------------------------------------------------
    # Dialog history management
    # ------------------------------------------------------------------

    def _append_history(self, role: str, content: str) -> None:
        self._dialog_history.append({"role": role, "content": content})
        if len(self._dialog_history) > self._history_max:
            del self._dialog_history[: -self._history_max]

    # ------------------------------------------------------------------
    # Barge-in / cancellation
    # ------------------------------------------------------------------

    async def _cancel_inflight(self) -> None:
        """Cancel the in-flight generation task and await its completion."""
        task = self._gen_task
        if task is not None and not task.done():
            task.cancel()
            try:
                await task
            except (asyncio.CancelledError, Exception):  # noqa: BLE001
                pass
        self._gen_task = None

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------

    async def cleanup(self) -> None:
        await self._cancel_inflight()
        if self._owns_client:
            try:
                await self._client.aclose()
            except Exception:  # noqa: BLE001
                pass
        parent_cleanup = getattr(super(), "cleanup", None)
        if callable(parent_cleanup):
            await parent_cleanup()


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

# Speaker-to-gender table (same mapping as voice-agent-worker).
_MALE_SPEAKERS = {"rahul", "arvind", "arjun", "hindi_m", "male"}


def _gender_for_speaker(speaker: str) -> str:
    """Return "male" or "female" for a Sarvam/ElevenLabs speaker name."""
    if not speaker:
        return "female"
    return "male" if speaker.strip().lower() in _MALE_SPEAKERS else "female"
