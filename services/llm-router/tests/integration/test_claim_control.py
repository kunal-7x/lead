from __future__ import annotations

from llm_router.claim_control import ClaimCheckResult, ClaimViolation
from llm_router.kb_client import FakeKbRetriever
from llm_router.models import BrainOutput
from llm_router.router import LLMRouter
from tests.conftest import make_req
from tests.fakes.fake_backends import FakeBackend


class BlockingClaimControl:
    async def check(self, **kwargs) -> ClaimCheckResult:
        return ClaimCheckResult(
            ok=False,
            action_taken="rewritten",
            rewritten="Let me check that and get back to you.",
            violations=[
                ClaimViolation(
                    claim_type="appreciation",
                    reason="guaranteed appreciation is forbidden",
                    action_taken="rewritten",
                )
            ],
        )


async def test_llm_router_rewrites_claim_control_violation(switcher):
    brain = BrainOutput(
        reply="Guaranteed appreciation 20 percent yearly.",
        lead_status="warm",
        lead_score=70,
        next_action="qualify",
        risk_level="safe",
        confidence=0.9,
        summary="Lead asked about investment.",
    )
    router = LLMRouter(
        {"groq_llama": FakeBackend(brain=brain)},
        switcher,
        FakeKbRetriever(),
        BlockingClaimControl(),
    )

    resp = await router.generate(make_req())

    assert resp.model_used == "groq_llama"
    assert resp.brain.reply == "Let me check that and get back to you."
    assert resp.brain.risk_level == "risky"
    assert resp.brain.should_handover_to_human is True
