"""Tests for the actions module (NATS event publishing).

Ported from services/voice-agent-worker/tests/test_actions.py.
These tests are pure (no pipecat, no NATS, no Redis needed).
"""

from __future__ import annotations

import pytest

from voice_agent_v2.actions import FakePublisher, handle_actions
from voice_agent_v2.models import BrainOutput, SessionContext


def _brain(**kwargs) -> BrainOutput:
    defaults = dict(
        reply="Transfer kar raha hoon.",
        lead_status="warm", lead_score=50,
        next_action="qualify", risk_level="safe",
        confidence=0.85, summary="test",
    )
    defaults.update(kwargs)
    return BrainOutput(**defaults)


def _ctx() -> SessionContext:
    return SessionContext(
        session_id="test-session",
        tenant_id="test-tenant",
        lead_id="lead-123",
        campaign_id="camp-456",
    )


@pytest.mark.asyncio
async def test_handover_event_published():
    pub = FakePublisher()
    brain = _brain(should_handover_to_human=True)
    await handle_actions(brain, _ctx(), pub)
    assert pub.count("call.handover.requested") == 1


@pytest.mark.asyncio
async def test_site_visit_event_published():
    pub = FakePublisher()
    brain = _brain(should_create_site_visit=True)
    await handle_actions(brain, _ctx(), pub)
    assert pub.count("call.site_visit.requested") == 1


@pytest.mark.asyncio
async def test_callback_event_published():
    pub = FakePublisher()
    brain = _brain(should_create_callback=True)
    await handle_actions(brain, _ctx(), pub)
    assert pub.count("call.callback.requested") == 1


@pytest.mark.asyncio
async def test_whatsapp_event_published():
    pub = FakePublisher()
    brain = _brain(should_send_whatsapp=True)
    await handle_actions(brain, _ctx(), pub)
    assert pub.count("call.whatsapp.requested") == 1


@pytest.mark.asyncio
async def test_lead_status_always_published():
    """lead.status.updated is always published, even with no action flags."""
    pub = FakePublisher()
    await handle_actions(_brain(), _ctx(), pub)
    assert pub.count("lead.status.updated") == 1


@pytest.mark.asyncio
async def test_multiple_actions_same_turn():
    pub = FakePublisher()
    brain = _brain(should_handover_to_human=True, should_send_whatsapp=True)
    await handle_actions(brain, _ctx(), pub)
    assert pub.count("call.handover.requested") == 1
    assert pub.count("call.whatsapp.requested") == 1


@pytest.mark.asyncio
async def test_no_extra_events_when_no_flags():
    """Only lead.status.updated is published when no action flags are set."""
    pub = FakePublisher()
    await handle_actions(_brain(), _ctx(), pub)
    assert pub.total == 1
    assert pub.count("lead.status.updated") == 1
