"""CsoProcessor — wraps the existing ConversationStateEngine inside a Pipecat
FrameProcessor.

On each finalized user transcription it fires ``engine.update(dialog_history,
user_turn)`` as a BACKGROUND task (latency-neutral, exactly like the old agent),
and stashes the resulting per-turn directive on a shared :class:`CallContext`.
The downstream ``LlmRouterProcessor`` reads ``call_ctx.system_prompt_suffix`` and
injects it into the llm-router payload.

Also surfaces the ``[LOW_CONF_TURN]`` suffix when barge-in confidence is below
``LOW_CONF_BARGE_IN_THRESHOLD`` (transcription confidence on the frame).

Shared-state design (per the spec's simplest option): both processors hold a
reference to one ``CallContext``; CsoProcessor writes the directive, the LLM
processor reads it. No cross-processor frame plumbing needed for the directive.
"""

from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass, field

from voice_agent.conversation_state import ConversationStateEngine
from voice_agent.models import SessionContext
from voice_agent.pipecat_runtime.config import PipecatSettings

# API-CHECK: pipecat.processors.frame_processor.FrameProcessor + FrameDirection,
# and pipecat.frames.frames.{Frame,TranscriptionFrame,StartInterruptionFrame}.
# Verify class/module paths against the installed pipecat-ai==1.3.*.
try:  # pragma: no cover - import shim exercised only with pipecat installed
    from pipecat.frames.frames import (  # type: ignore
        Frame,
        StartInterruptionFrame,
        TranscriptionFrame,
    )
    from pipecat.processors.frame_processor import (  # type: ignore
        FrameDirection,
        FrameProcessor,
    )
except Exception:  # noqa: BLE001 - allow import without pipecat (scaffold/tests)
    from voice_agent.pipecat_runtime._pipecat_shim import (
        Frame,
        FrameDirection,
        FrameProcessor,
        StartInterruptionFrame,
        TranscriptionFrame,
    )

logger = logging.getLogger(__name__)

# Suffix appended for low-confidence barge-in turns; llm-router treats it as a
# hint to be terse/clarifying. Matches the old agent's [LOW_CONF_TURN] marker.
_LOW_CONF_SUFFIX = "[LOW_CONF_TURN]"


@dataclass
class CallContext:
    """Mutable per-call state shared between the CSO and LLM processors.

    This is the single object both processors read/write. It carries the
    SessionContext, the rolling dialog_history (window-trimmed by the LLM
    processor), the accumulated collected_slots, and the latest CSO directive.
    """

    session: SessionContext
    settings: PipecatSettings
    dialog_history: list[dict] = field(default_factory=list)
    collected_slots: dict = field(default_factory=dict)
    # Written by CsoProcessor each turn; read by LlmRouterProcessor.
    cso_directive: str = ""
    last_turn_low_conf: bool = False
    # Per-turn correlation for analytics/turn records.
    turn_index: int = 0

    def system_prompt_suffix(self) -> str:
        """Compose the directive + optional low-conf marker for the LLM payload."""
        parts = [p for p in (self.cso_directive,) if p]
        if self.last_turn_low_conf:
            parts.append(_LOW_CONF_SUFFIX)
        return " ".join(parts).strip()


class CsoProcessor(FrameProcessor):
    """Fire the CSO classification per user turn; stash the directive on CallContext."""

    def __init__(self, call_ctx: CallContext) -> None:
        super().__init__()
        self._ctx = call_ctx
        s = call_ctx.settings
        self._engine = ConversationStateEngine(
            groq_api_key=s.groq_api_key,
            groq_api_key_2=s.groq_api_key_2,
            timeout=s.cso_timeout_s,
        )
        self._threshold = s.low_conf_barge_in_threshold
        self._pending: asyncio.Task | None = None

    async def process_frame(self, frame: "Frame", direction: "FrameDirection") -> None:
        # API-CHECK: FrameProcessor.process_frame signature + the requirement to
        # call super().process_frame(...) and push_frame(...). Verify vs pipecat 1.3.
        await super().process_frame(frame, direction)

        if isinstance(frame, TranscriptionFrame):
            user_turn = (getattr(frame, "text", "") or "").strip()
            if user_turn:
                # Low-confidence marker: short transcript or low STT confidence.
                conf = getattr(frame, "confidence", None)
                low_conf = (
                    conf is not None and conf < self._threshold
                    and len(user_turn.split()) <= 2
                )
                self._ctx.last_turn_low_conf = low_conf
                self._fire_cso(user_turn)

        elif isinstance(frame, StartInterruptionFrame):
            # Caller barged in — nothing to classify; the LLM processor cancels
            # its in-flight SSE on the same frame.
            pass

        await self.push_frame(frame, direction)

    def _fire_cso(self, user_turn: str) -> None:
        """Start CSO classification in the background (latency-neutral).

        We snapshot dialog_history BEFORE the current turn (the engine expects the
        prior context + the new user turn), then stash the directive when done.
        On any failure the engine falls back to neutral defaults internally.
        """
        history_snapshot = list(self._ctx.dialog_history)

        async def _run() -> None:
            try:
                await self._engine.update(history_snapshot, user_turn)
            except Exception as exc:  # noqa: BLE001 - never break the turn
                logger.debug("CSO update failed (%r)", exc)
            finally:
                self._ctx.cso_directive = self._engine.directive()

        # Fire-and-forget; bounded by the engine's own timeout. Keep a handle so a
        # rapid next turn can supersede the previous (best-effort cancel).
        if self._pending is not None and not self._pending.done():
            self._pending.cancel()
        self._pending = asyncio.create_task(_run())

    async def cleanup(self) -> None:
        # API-CHECK: FrameProcessor.cleanup() hook name in pipecat 1.3.
        if self._pending is not None and not self._pending.done():
            self._pending.cancel()
        try:
            await self._engine.aclose()
        except Exception:  # noqa: BLE001
            pass
        parent_cleanup = getattr(super(), "cleanup", None)
        if parent_cleanup is not None:
            await parent_cleanup()
