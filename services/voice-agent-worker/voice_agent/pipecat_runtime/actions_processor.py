"""ActionsProcessor — taps BrainOutputFrame and replicates the old worker's
actions → NATS behaviour exactly.

Per BrainOutput it calls ``actions.handle_actions`` (publishes
``lead.status.updated`` every turn + the gated ``call.handover/site_visit/callback/
whatsapp.requested`` subjects), and records ``call.turn.recorded`` = asdict(CallTurn).
On ``EndFrame`` it emits the enriched ``call.completed`` once, with the same payload
shape the old agent._finalize builds (transcript rebuilt from dialog_history).

Reuses the existing NATS publisher (actions.Publisher protocol) passed in — no new
event plumbing. NATS subjects/payloads are byte-for-byte the old ones.
"""

from __future__ import annotations

import json
import logging
import time
from dataclasses import asdict

from voice_agent.actions import handle_actions
from voice_agent.models import BrainOutput, CallTurn
from voice_agent.pipecat_runtime.cso_processor import CallContext
from voice_agent.pipecat_runtime.frames import BrainOutputFrame

# API-CHECK: Frame / EndFrame / FrameProcessor / FrameDirection vs pipecat 1.3.
try:  # pragma: no cover - exercised only with pipecat installed
    from pipecat.frames.frames import EndFrame, Frame  # type: ignore
    from pipecat.processors.frame_processor import (  # type: ignore
        FrameDirection,
        FrameProcessor,
    )
except Exception:  # noqa: BLE001
    from voice_agent.pipecat_runtime._pipecat_shim import (
        EndFrame,
        Frame,
        FrameDirection,
        FrameProcessor,
    )

logger = logging.getLogger(__name__)


class ActionsProcessor(FrameProcessor):
    """Publish brain-driven NATS events; record turns; emit call.completed at end."""

    def __init__(self, call_ctx: CallContext, publisher, turn_store=None) -> None:
        super().__init__()
        self._ctx = call_ctx
        self._publisher = publisher
        self._store = turn_store  # optional recorder.TurnStore (NatsTurnStore)
        self._last_brain: BrainOutput | None = None
        self._turn_index = 0
        self._call_start_ts = time.time()
        self._completed = False

    async def process_frame(self, frame: "Frame", direction: "FrameDirection") -> None:
        await super().process_frame(frame, direction)

        if isinstance(frame, BrainOutputFrame) and isinstance(frame.brain, BrainOutput):
            await self._on_brain(frame.brain)
            # BrainOutputFrame is internal metadata — do not forward to transport.
            return

        if isinstance(frame, EndFrame):
            await self._on_end()

        await self.push_frame(frame, direction)

    async def _on_brain(self, brain: BrainOutput) -> None:
        self._last_brain = brain
        # 1) Gated action subjects + lead.status.updated (every turn).
        try:
            await handle_actions(brain, self._ctx.session, self._publisher)
        except Exception as exc:  # noqa: BLE001
            logger.warning("handle_actions failed (%r)", exc)

        # 2) Per-turn transcript record (call.turn.recorded = asdict(CallTurn)).
        turn = CallTurn(
            session_id=self._ctx.session.session_id,
            turn_index=self._turn_index,
            speaker="agent",
            reply=brain.reply,
            lead_status=brain.lead_status,
            next_action=brain.next_action,
            brain_json=brain.model_dump(),
            tts_engine="",
        )
        self._turn_index += 1
        try:
            if self._store is not None:
                await self._store.record_turn(turn)
            else:
                await self._publisher.publish(
                    "call.turn.recorded",
                    json.dumps(asdict(turn), ensure_ascii=True).encode(),
                )
        except Exception as exc:  # noqa: BLE001
            logger.warning("record_turn failed (%r)", exc)

    async def _on_end(self) -> None:
        if self._completed:
            return
        self._completed = True
        brain = self._last_brain
        outcome = brain.next_action if brain else "unknown"
        summary = brain.summary if brain else "Call ended"
        transcript = self._build_transcript()
        duration_s = round(time.time() - self._call_start_ts, 3)
        payload = {
            "session_id": self._ctx.session.session_id,
            "tenant_id": self._ctx.session.tenant_id,
            "campaign_id": self._ctx.session.campaign_id,
            "lead_id": self._ctx.session.lead_id,
            "project_id": self._ctx.session.project_id,
            "outcome": outcome,
            "status": "completed",
            "summary": summary,
            "lead_status": brain.lead_status if brain else "",
            "lead_score": brain.lead_score if brain else 0,
            "duration_s": duration_s,
            "turn_count": self._turn_index,
            "transcript": transcript,
        }
        try:
            await self._publisher.publish("call.completed", json.dumps(payload).encode())
        except Exception as exc:  # noqa: BLE001
            logger.warning("call.completed publish failed (%r)", exc)

    def _build_transcript(self) -> list[dict]:
        """Flatten dialog_history → ordered [{speaker, text}] (caller/agent)."""
        out: list[dict] = []
        for msg in self._ctx.dialog_history:
            speaker = "caller" if msg.get("role") == "user" else "agent"
            out.append({"speaker": speaker, "text": msg.get("content", "")})
        return out

    async def cleanup(self) -> None:
        # Ensure call.completed fires even if no explicit EndFrame was seen.
        await self._on_end()
        parent_cleanup = getattr(super(), "cleanup", None)
        if parent_cleanup is not None:
            await parent_cleanup()
