from eval_harness import EvalHarness, seed_store


def test_suite_run_produces_deterministic_scores() -> None:
    first = EvalHarness(seed_store()).run_suite("prompt_v14", "groq_llama", "default", seed=42)
    second = EvalHarness(seed_store()).run_suite("prompt_v14", "groq_llama", "default", seed=42)

    assert first.aggregate_score == second.aggregate_score
    assert first.high_severity_score == second.high_severity_score
    assert first.case_scores == second.case_scores
    assert first.green is True
