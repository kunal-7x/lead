from __future__ import annotations

import pytest
from pydantic import ValidationError

from llm_router.models import BrainOutput, FALLBACK_BRAIN
from llm_router.router import LLMRouter
from llm_router.kb_client import FakeKbRetriever
from tests.fakes.fake_backends import FakeBackend
from tests.conftest import make_req


async def _run(brain: BrainOutput, switcher) -> BrainOutput:
    backend = FakeBackend(brain=brain)
    router = LLMRouter({"groq_llama": backend}, switcher, FakeKbRetriever())
    resp = await router.generate(make_req())
    return resp.brain


async def test_valid_brain_passthrough(switcher):
    """Valid brain JSON output passes through unchanged."""
    resp_brain = await _run(BrainOutput(
        reply="2BHK 60 lakh mein available hai.",
        lead_status="warm",
        lead_score=70,
        next_action="qualify",
        risk_level="safe",
        confidence=0.90,
        summary="Interested in 2BHK",
    ), switcher)
    assert resp_brain.lead_status == "warm"
    assert resp_brain.confidence == 0.90


async def test_all_lead_statuses_valid(switcher):
    """All valid lead_status values pass schema validation."""
    statuses = ["hot", "warm", "cold", "call_later", "not_interested",
                "wrong_number", "opt_out", "broker", "fake", "needs_human_review"]
    for status in statuses:
        brain = BrainOutput(
            reply="Test reply.",
            lead_status=status,
            lead_score=50,
            next_action="qualify",
            risk_level="safe",
            confidence=0.80,
            summary="test",
        )
        resp = await _run(brain, switcher)
        assert resp.lead_status == status


async def test_all_next_actions_valid(switcher):
    """All valid next_action values pass schema validation."""
    actions = ["qualify", "book_site_visit", "callback", "handover", "end_call", "opt_out"]
    for action in actions:
        brain = BrainOutput(
            reply="Test.",
            lead_status="warm",
            lead_score=50,
            next_action=action,
            risk_level="safe",
            confidence=0.80,
            summary="test",
        )
        resp = await _run(brain, switcher)
        assert resp.next_action == action


def test_invalid_lead_status_rejected():
    with pytest.raises(ValidationError):
        BrainOutput(
            reply="X", lead_status="invalid_status",
            lead_score=50, next_action="qualify",
            risk_level="safe", confidence=0.8, summary="x"
        )


def test_invalid_next_action_rejected():
    with pytest.raises(ValidationError):
        BrainOutput(
            reply="X", lead_status="warm",
            lead_score=50, next_action="fly_away",
            risk_level="safe", confidence=0.8, summary="x"
        )


async def test_all_backends_fail_returns_fallback(switcher):
    """All backends fail → FALLBACK_BRAIN returned."""
    bad = FakeBackend(raise_error=True)
    router = LLMRouter({"groq_llama": bad}, switcher, FakeKbRetriever())
    resp = await router.generate(make_req())
    assert resp.brain.lead_status == "needs_human_review"
    assert resp.brain.should_handover_to_human is True
    assert resp.model_used == "fallback"


async def test_fallback_brain_is_valid():
    """FALLBACK_BRAIN itself is a valid BrainOutput."""
    assert FALLBACK_BRAIN.lead_status == "needs_human_review"
    assert FALLBACK_BRAIN.should_handover_to_human is True
