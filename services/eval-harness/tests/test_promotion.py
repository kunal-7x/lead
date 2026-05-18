import pytest

from eval_harness import PromotionBlocked


def test_green_run_can_promote_prompt(harness) -> None:
    run = harness.run_suite("prompt_v14", "groq_llama", "default")
    harness.promote_prompt("prompt_v14", run.id, actor_role="internal_admin")
    assert harness.store.active_prompt_version == "prompt_v14"


def test_only_internal_admin_can_promote(harness) -> None:
    run = harness.run_suite("prompt_v14", "groq_llama", "default")
    with pytest.raises(PermissionError):
        harness.promote_prompt("prompt_v14", run.id, actor_role="client_owner")


def test_non_green_run_cannot_promote(harness) -> None:
    run = harness.run_suite("prompt_regression_5pct", "groq_llama", "default")
    with pytest.raises(PromotionBlocked):
        harness.promote_prompt("prompt_regression_5pct", run.id, actor_role="internal_admin")
