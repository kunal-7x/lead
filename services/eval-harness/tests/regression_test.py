import pytest

from eval_harness import EvalHarness, PromotionBlocked, seed_store


def test_regression_gate_blocks_high_severity_drop() -> None:
    harness = EvalHarness(seed_store())
    baseline = harness.run_suite("prompt_v14", "groq_llama", "default")
    candidate = harness.run_suite("prompt_regression_5pct", "groq_llama", "default")

    with pytest.raises(PromotionBlocked, match="dropped"):
        harness.assert_promotable(candidate.id, baseline.id)

    assert harness.store.active_prompt_version is None
