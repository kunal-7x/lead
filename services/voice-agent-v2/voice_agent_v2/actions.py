"""NATS event publishing for agent actions.

Ported from services/voice-agent-worker/voice_agent/actions.py.
Publishes NATS events based on brain output action flags.
"""

from __future__ import annotations

import json
from typing import Protocol

from voice_agent_v2.models import BrainOutput, SessionContext


class Publisher(Protocol):
    async def publish(self, subject: str, payload: bytes) -> None: ...


class FakePublisher:
    """In-memory NATS publisher for tests."""

    def __init__(self) -> None:
        self.messages: dict[str, list[bytes]] = {}
        self.total = 0

    async def publish(self, subject: str, payload: bytes) -> None:
        self.messages.setdefault(subject, []).append(payload)
        self.total += 1

    def count(self, subject: str) -> int:
        return len(self.messages.get(subject, []))


async def handle_actions(
    brain: BrainOutput,
    ctx: SessionContext,
    publisher: Publisher,
) -> None:
    """Publish NATS events based on brain output action flags."""
    base = {
        "session_id": ctx.session_id,
        "tenant_id": ctx.tenant_id,
        "lead_id": ctx.lead_id,
        "campaign_id": ctx.campaign_id,
    }

    if brain.should_handover_to_human:
        await publisher.publish(
            "call.handover.requested",
            json.dumps({**base, "reason": brain.summary}).encode(),
        )

    if brain.should_create_site_visit:
        await publisher.publish(
            "call.site_visit.requested",
            json.dumps(base).encode(),
        )

    if brain.should_create_callback:
        await publisher.publish(
            "call.callback.requested",
            json.dumps(base).encode(),
        )

    if brain.should_send_whatsapp:
        await publisher.publish(
            "call.whatsapp.requested",
            json.dumps({**base, "reply": brain.reply}).encode(),
        )

    await publisher.publish(
        "lead.status.updated",
        json.dumps({**base, "status": brain.lead_status, "score": brain.lead_score}).encode(),
    )
