from __future__ import annotations

from dataclasses import dataclass, field
from typing import Protocol

from voice_agent.models import CallTurn, STTResult, BrainOutput, SessionContext


class TurnStore(Protocol):
    async def record_turn(self, turn: CallTurn) -> None: ...
    async def complete_call(self, session_id: str, summary: str, outcome: str) -> None: ...


@dataclass
class FakeTurnStore:
    """In-memory turn recorder for tests."""
    turns: list[CallTurn] = field(default_factory=list)
    completed: list[dict] = field(default_factory=list)

    async def record_turn(self, turn: CallTurn) -> None:
        self.turns.append(turn)

    async def complete_call(self, session_id: str, summary: str, outcome: str) -> None:
        self.completed.append({"session_id": session_id,
                               "summary": summary, "outcome": outcome})


async def record_turn(
    store: TurnStore,
    ctx: SessionContext,
    turn_index: int,
    stt: STTResult,
    brain: BrainOutput,
    tts_engine: str,
    cache_hit: bool,
) -> CallTurn:
    """Build a CallTurn and persist it."""
    turn = CallTurn(
        session_id=ctx.session_id,
        turn_index=turn_index,
        speaker="caller",
        transcript=stt.text,
        confidence=stt.confidence,
        stt_engine=stt.engine_used,
        reply=brain.reply,
        lead_status=brain.lead_status,
        next_action=brain.next_action,
        brain_json=brain.model_dump(),
        tts_engine=tts_engine,
        cache_hit=cache_hit,
    )
    await store.record_turn(turn)
    return turn
