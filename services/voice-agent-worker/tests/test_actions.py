from __future__ import annotations

from voice_agent.actions import FakePublisher, handle_actions
from voice_agent.models import BrainOutput
from tests.conftest import make_ctx, make_loop, run_loop
from tests.fakes.fake_services import FakeLLM, FakeFreeSwitchWS


def _brain(**kwargs) -> BrainOutput:
    defaults = dict(
        reply="Transfer kar raha hoon.",
        lead_status="warm", lead_score=50,
        next_action="qualify", risk_level="safe",
        confidence=0.85, summary="test",
    )
    defaults.update(kwargs)
    return BrainOutput(**defaults)


async def test_handover_event_published():
    """should_handover_to_human=True → call.handover.requested published."""
    pub = FakePublisher()
    llm = FakeLLM(handover=True, next_action="handover")
    loop = make_loop(llm=llm, publisher=pub)

    ws = FakeFreeSwitchWS()
    await run_loop(loop, ws.all_chunks())

    assert pub.count("call.handover.requested") == 1


async def test_site_visit_event_published():
    pub = FakePublisher()
    brain = _brain(should_create_site_visit=True)
    ctx = make_ctx()
    await handle_actions(brain, ctx, pub)
    assert pub.count("call.site_visit.requested") == 1


async def test_callback_event_published():
    pub = FakePublisher()
    brain = _brain(should_create_callback=True)
    ctx = make_ctx()
    await handle_actions(brain, ctx, pub)
    assert pub.count("call.callback.requested") == 1


async def test_whatsapp_event_published():
    pub = FakePublisher()
    brain = _brain(should_send_whatsapp=True)
    ctx = make_ctx()
    await handle_actions(brain, ctx, pub)
    assert pub.count("call.whatsapp.requested") == 1


async def test_lead_status_always_published():
    """lead.status.updated is always published, even with no action flags."""
    pub = FakePublisher()
    await handle_actions(_brain(), make_ctx(), pub)
    assert pub.count("lead.status.updated") == 1


async def test_call_completed_event():
    """call.completed is published when call ends."""
    pub = FakePublisher()
    loop = make_loop(publisher=pub)
    ws = FakeFreeSwitchWS()
    await run_loop(loop, ws.all_chunks())
    assert pub.count("call.completed") >= 1


async def test_multiple_actions_same_turn():
    pub = FakePublisher()
    brain = _brain(should_handover_to_human=True, should_send_whatsapp=True)
    await handle_actions(brain, make_ctx(), pub)
    assert pub.count("call.handover.requested") == 1
    assert pub.count("call.whatsapp.requested") == 1
