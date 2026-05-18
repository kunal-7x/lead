from __future__ import annotations

from dataclasses import replace
from statistics import mean

from eval_harness.models import BrainOutput, EvaluationResult, GoldenCase, PromptTestRun, summary_fingerprint
from eval_harness.store import InMemoryStore


class PromotionBlocked(Exception):
    pass


class DeterministicModel:
    def generate(
        self,
        case: GoldenCase,
        prompt_version: str,
        model_version: str,
        seed: int,
    ) -> BrainOutput:
        output = case.expected_output
        if "regression" in prompt_version and case.severity == "high":
            return output.with_regression()
        if "summary_shift" in prompt_version:
            return replace(output, summary=f"{output.summary} extra detail {seed}")
        return output


class EvalHarness:
    def __init__(self, store: InMemoryStore, model: DeterministicModel | None = None) -> None:
        self.store = store
        self.model = model or DeterministicModel()

    def run_suite(
        self,
        prompt_version: str,
        model_version: str,
        suite_id: str,
        seed: int = 7,
    ) -> PromptTestRun:
        cases = self.store.list_cases(suite_id)
        if not cases:
            raise ValueError(f"no golden cases for suite {suite_id}")

        run_id = f"run-{suite_id}-{prompt_version}-{model_version}-{seed}-{len(self.store.prompt_test_runs) + 1}"
        evaluations = [
            self.evaluate_case(run_id, case, self.model.generate(case, prompt_version, model_version, seed))
            for case in cases
        ]
        aggregate_score = round(mean(evaluation.score for evaluation in evaluations), 4)
        high_scores = [evaluation.score for evaluation in evaluations if evaluation.severity == "high"]
        high_severity_score = round(mean(high_scores), 4) if high_scores else 1.0
        run = PromptTestRun(
            id=run_id,
            prompt_version=prompt_version,
            model_version=model_version,
            suite_id=suite_id,
            aggregate_score=aggregate_score,
            high_severity_score=high_severity_score,
            green=all(evaluation.score >= 1.0 for evaluation in evaluations if evaluation.severity == "high"),
            case_scores={evaluation.case_id: evaluation.score for evaluation in evaluations},
        )
        self.store.save_run(run, evaluations)
        return run

    def evaluate_case(self, run_id: str, case: GoldenCase, output: BrainOutput) -> EvaluationResult:
        checks: list[tuple[str, bool, float]] = [
            ("next_action", output.next_action == case.expected_next_action, 0.3),
            ("risk_level", output.risk_level == case.expected_risk_level, 0.25),
            ("lead_status", output.lead_status == case.expected_lead_status, 0.25),
            (
                "summary_fingerprint",
                summary_fingerprint(output.summary) == case.expected_summary_fingerprint,
                0.2,
            ),
        ]
        score = round(sum(weight for _, passed, weight in checks if passed), 4)
        failures = tuple(name for name, passed, _ in checks if not passed)
        return EvaluationResult(
            id=f"{run_id}-{case.id}",
            run_id=run_id,
            case_id=case.id,
            severity=case.severity,
            score=score,
            failures=failures,
            output=output,
        )

    def run_on_prompt_change(self, prompt_version: str, model_version: str, suite_id: str = "default") -> PromptTestRun:
        return self.run_suite(prompt_version, model_version, suite_id)

    def run_on_model_swap(self, prompt_version: str, model_version: str, suite_id: str = "default") -> PromptTestRun:
        return self.run_suite(prompt_version, model_version, suite_id)

    def assert_promotable(self, candidate_run_id: str, baseline_run_id: str) -> None:
        candidate = self.store.get_run(candidate_run_id)
        baseline = self.store.get_run(baseline_run_id)
        candidate_evals = self.store.evaluations_for_run(candidate.id)
        baseline_evals = self.store.evaluations_for_run(baseline.id)

        for case_id, baseline_eval in baseline_evals.items():
            if baseline_eval.severity != "high":
                continue
            candidate_eval = candidate_evals[case_id]
            if candidate_eval.score < baseline_eval.score:
                raise PromotionBlocked(
                    f"{case_id} dropped from {baseline_eval.score:.2f} to {candidate_eval.score:.2f}"
                )
        if not candidate.green:
            raise PromotionBlocked(f"{candidate.id} is not green")

    def promote_prompt(self, prompt_version: str, run_id: str, actor_role: str) -> None:
        if actor_role not in {"internal_admin", "super_admin"}:
            raise PermissionError("internal_admin role required")
        run = self.store.get_run(run_id)
        if run.prompt_version != prompt_version:
            raise ValueError("run does not belong to prompt version")
        if not run.green:
            raise PromotionBlocked("green suite run required")
        self.store.active_prompt_version = prompt_version
