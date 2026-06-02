"""LlmRouterProcessor — the brain lives in llm-router; this is Pipecat's LLM stage.

On each finalized user transcription it:
  1. Appends the user turn to ``call_ctx.dialog_history`` (window LLM_HISTORY_MAX_MSGS=12).
  2. Builds the EXACT llm-router payload (see PIPECAT_RUNTIME_DESIGN.md contract).
  3. POSTs to ``LLM_ROUTER_URL/v1/llm/generate/stream_text`` (SSE) → pushes
     ProsodyShaper-cleaned ``TextFrame`` tokens DOWNSTREAM to TTS, bracketed by
     ``LLMFullResponseStartFrame`` / ``LLMFullResponseEndFrame``.
  4. FIRES a PARALLEL POST to ``/v1/llm/generate`` → parses ``body["brain"]`` into a
     ``BrainOutput`` → emits a ``BrainOutputFrame`` for ActionsProcessor, and folds
     any returned slots into ``call_ctx.collected_slots``.
  5. Appends the assistant reply to dialog_history.
  6. On ``StartInterruptionFrame`` cancels the in-flight SSE + parallel request.

The payload is built identically to the old worker's HttpLLMClient._build_payload
(same keys, same persona_gender derivation, same suffix/slots/campaign gating) so
the brain interface is byte-for-byte the same — only the transport stage changed.
"""

from __future__ import annotations

import asyncio
import json
import logging

import httpx

from voice_agent.models import BrainOutput
from voice_agent.pipecat_runtime.cso_processor import CallContext
from voice_agent.pipecat_runtime.frames import BrainOutputFrame
from voice_agent.prosody import ProsodyShaper
from voice_agent.voice_gender import gender_for_speaker

# API-CHECK: frame classes + FrameProcessor base + FrameDirection — verify module
# paths/names against pipecat-ai==1.3.* (see _pipecat_shim docstring).
try:  # pragma: no cover - exercised only with pipecat installed
    from pipecat.frames.frames import (  # type: ignore
        Frame,
        LLMFullResponseEndFrame,
        LLMFullResponseStartFrame,
        StartInterruptionFrame,
        TextFrame,
        TranscriptionFrame,
    )
    from pipecat.processors.frame_processor import (  # type: ignore
        FrameDirection,
        FrameProcessor,
    )
except Exception:  # noqa: BLE001
    from voice_agent.pipecat_runtime._pipecat_shim import (
        Frame,
        FrameDirection,
        FrameProcessor,
        LLMFullResponseEndFrame,
        LLMFullResponseStartFrame,
        StartInterruptionFrame,
        TextFrame,
        TranscriptionFrame,
    )

logger = logging.getLogger(__name__)


class LlmRouterProcessor(FrameProcessor):
    """Custom LLM stage that defers generation to llm-router (keeps the brain)."""

    def __init__(
        self,
        call_ctx: CallContext,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        super().__init__()
        self._ctx = call_ctx
        self._base_url = call_ctx.settings.llm_router_url.rstrip("/")
        self._history_max = max(2, int(call_ctx.settings.llm_history_max_msgs))
        self._shaper = ProsodyShaper()
        self._client = http_client or httpx.AsyncClient(timeout=30.0)
        self._owns_client = http_client is None
        # In-flight generation task (SSE + parallel metadata). Cancelled on barge-in.
        self._gen_task: asyncio.Task | None = None

    # ── payload (MUST match llm-router contract exactly) ──────────────────────
    def _build_payload(self, user_turn: str) -> dict:
        ctx = self._ctx.session
        payload: dict = {
            "user_turn": user_turn,
            "lang": ctx.lang,
            "tenant_id": ctx.tenant_id,
            "session_id": ctx.session_id,
            "project_id": ctx.project_id,
            "system_prompt_version": ctx.system_prompt_version,
            "dialog_history": list(self._ctx.dialog_history),
            "persona_gender": gender_for_speaker(ctx.voice_profile_id),
        }
        if self._ctx.collected_slots:
            payload["collected_slots"] = dict(self._ctx.collected_slots)
        suffix = self._ctx.system_prompt_suffix()
        if suffix:
            payload["system_prompt_suffix"] = suffix
        if ctx.campaign_context:
            payload["campaign_context"] = ctx.campaign_context
        return payload

    # ── frame entrypoint ──────────────────────────────────────────────────────
    async def process_frame(self, frame: "Frame", direction: "FrameDirection") -> None:
        await super().process_frame(frame, direction)

        if isinstance(frame, StartInterruptionFrame):
            await self._cancel_inflight()
            await self.push_frame(frame, direction)
            return

        if isinstance(frame, TranscriptionFrame):
            user_turn = (getattr(frame, "text", "") or "").strip()
            if user_turn:
                # Supersede any still-running generation (defensive; barge-in
                # normally cancels first via StartInterruptionFrame).
                await self._cancel_inflight()
                self._gen_task = asyncio.create_task(self._generate(user_turn))
            # Do NOT forward the raw TranscriptionFrame downstream to TTS; the
            # reply TextFrames are what TTS should speak.
            return

        # Pass everything else through unchanged.
        await self.push_frame(frame, direction)

    # ── generation: streaming reply + parallel brain metadata ─────────────────
    async def _generate(self, user_turn: str) -> None:
        # Record the user turn BEFORE building the payload's dialog_history? No —
        # llm-router expects dialog_history to be the PRIOR context and user_turn
        # separately, exactly like the old client. Append AFTER building payload.
        payload = self._build_payload(user_turn)
        self._append_history("user", user_turn)

        # Kick the parallel metadata POST first so it overlaps the SSE stream.
        brain_task = asyncio.create_task(self._fetch_brain(payload))

        reply_text_parts: list[str] = []
        started = False
        try:
            async with self._client.stream(
                "POST",
                f"{self._base_url}/v1/llm/generate/stream_text",
                json=payload,
                timeout=30.0,
            ) as resp:
                resp.raise_for_status()
                await self.push_frame(LLMFullResponseStartFrame(), FrameDirection.DOWNSTREAM)
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
                    # ProsodyShaper.shape() sanitises markdown/symbols before TTS.
                    shaped = self._shaper.shape(token)
                    if shaped:
                        reply_text_parts.append(shaped)
                        await self.push_frame(TextFrame(text=shaped), FrameDirection.DOWNSTREAM)
        except asyncio.CancelledError:
            # Barge-in / supersede: close the response bracket if we opened it,
            # then re-raise so the task ends cleanly.
            if started:
                await self.push_frame(LLMFullResponseEndFrame(), FrameDirection.DOWNSTREAM)
            brain_task.cancel()
            raise
        except Exception as exc:  # noqa: BLE001
            logger.warning("llm-router stream_text failed (%r)", exc)
        finally:
            if started:
                await self.push_frame(LLMFullResponseEndFrame(), FrameDirection.DOWNSTREAM)

        reply = " ".join(reply_text_parts).strip()
        if reply:
            self._append_history("assistant", reply)

        # Collect the parallel brain metadata and hand it to ActionsProcessor.
        try:
            brain = await brain_task
        except asyncio.CancelledError:
            return
        except Exception as exc:  # noqa: BLE001
            logger.warning("llm-router brain fetch failed (%r)", exc)
            brain = None
        if brain is not None:
            self._fold_slots(brain)
            await self.push_frame(BrainOutputFrame(brain=brain), FrameDirection.DOWNSTREAM)
        self._ctx.turn_index += 1

    async def _fetch_brain(self, payload: dict) -> BrainOutput | None:
        """Parallel POST /v1/llm/generate → BrainOutput from body['brain']."""
        resp = await self._client.post(
            f"{self._base_url}/v1/llm/generate", json=payload, timeout=20.0
        )
        resp.raise_for_status()
        body = resp.json()
        return BrainOutput(**body["brain"])

    # ── helpers ────────────────────────────────────────────────────────────────
    def _append_history(self, role: str, content: str) -> None:
        self._ctx.dialog_history.append({"role": role, "content": content})
        # Window trim: keep only the last N messages (post-call transcript is
        # rebuilt elsewhere; here we bound the LLM context like the old agent).
        if len(self._ctx.dialog_history) > self._history_max:
            del self._ctx.dialog_history[:-self._history_max]

    def _fold_slots(self, brain: BrainOutput) -> None:
        """Accumulate slot fields across the call (budget/location/etc.)."""
        slots = self._ctx.collected_slots
        if brain.budget is not None:
            slots["budget"] = brain.budget
        if brain.location_pref is not None:
            slots["location_pref"] = brain.location_pref
        if brain.property_type is not None:
            slots["property_type"] = brain.property_type
        if brain.timeline_days is not None:
            slots["timeline_days"] = brain.timeline_days
        if brain.purpose is not None:
            slots["purpose"] = brain.purpose

    async def _cancel_inflight(self) -> None:
        if self._gen_task is not None and not self._gen_task.done():
            self._gen_task.cancel()
            try:
                await self._gen_task
            except (asyncio.CancelledError, Exception):  # noqa: BLE001
                pass
        self._gen_task = None

    async def cleanup(self) -> None:
        await self._cancel_inflight()
        if self._owns_client:
            try:
                await self._client.aclose()
            except Exception:  # noqa: BLE001
                pass
        parent_cleanup = getattr(super(), "cleanup", None)
        if parent_cleanup is not None:
            await parent_cleanup()
