from __future__ import annotations

from dataclasses import replace

from eval_harness.models import (
    AIFeedback,
    BrainOutput,
    EvaluationResult,
    GoldenCase,
    HallucinationIncident,
    HumanReviewItem,
    PromptTestRun,
    summary_fingerprint,
)


class InMemoryStore:
    def __init__(self) -> None:
        self.golden_test_cases: dict[str, GoldenCase] = {}
        self.prompt_test_runs: dict[str, PromptTestRun] = {}
        self.ai_evaluations: list[EvaluationResult] = []
        self.ai_feedback: list[AIFeedback] = []
        self.hallucination_incidents: list[HallucinationIncident] = []
        self.human_review_queue: dict[str, HumanReviewItem] = {}
        self.active_prompt_version: str | None = None

    def add_case(self, case: GoldenCase) -> None:
        self.golden_test_cases[case.id] = case

    def list_cases(self, suite_id: str) -> list[GoldenCase]:
        return sorted(
            [case for case in self.golden_test_cases.values() if case.suite_id == suite_id],
            key=lambda case: case.id,
        )

    def save_run(self, run: PromptTestRun, evaluations: list[EvaluationResult]) -> None:
        self.prompt_test_runs[run.id] = run
        self.ai_evaluations.extend(evaluations)

    def get_run(self, run_id: str) -> PromptTestRun:
        return self.prompt_test_runs[run_id]

    def evaluations_for_run(self, run_id: str) -> dict[str, EvaluationResult]:
        return {evaluation.case_id: evaluation for evaluation in self.ai_evaluations if evaluation.run_id == run_id}

    def enqueue_review(self, item: HumanReviewItem) -> None:
        self.human_review_queue[item.id] = item

    def update_review_status(self, item_id: str, status: str) -> None:
        item = self.human_review_queue[item_id]
        self.human_review_queue[item_id] = replace(item, status=status)

    def save_feedback(self, feedback: AIFeedback) -> None:
        self.ai_feedback.append(feedback)

    def save_incident(self, incident: HallucinationIncident) -> None:
        self.hallucination_incidents.append(incident)


def seed_store() -> InMemoryStore:
    store = InMemoryStore()
    case_hot = BrainOutput(
        reply="Saturday 11 baje site visit book kar deta hoon.",
        next_action="book_site_visit",
        risk_level="safe",
        lead_status="hot",
        summary="Buyer wants 2BHK visit on Saturday",
        confidence=0.94,
    )
    case_handover = BrainOutput(
        reply="Main aapko senior advisor se connect karwa deta hoon.",
        next_action="handover",
        risk_level="risky",
        lead_status="needs_human_review",
        summary="Loan guarantee claim needs human review",
        confidence=0.73,
    )
    case_callback = BrainOutput(
        reply="Theek hai, kal shaam callback schedule kar deta hoon.",
        next_action="callback",
        risk_level="safe",
        lead_status="warm",
        summary="Lead asked for callback tomorrow evening",
        confidence=0.88,
    )

    for case in (
        GoldenCase(
            id="high-site-visit",
            suite_id="default",
            severity="high",
            dialog=["Need 2BHK near metro, budget 85 lakh", "Saturday visit possible?"],
            expected_next_action="book_site_visit",
            expected_risk_level="safe",
            expected_lead_status="hot",
            expected_summary_fingerprint=summary_fingerprint(case_hot.summary),
            expected_output=case_hot,
            tags=("site_visit", "hot_lead"),
        ),
        GoldenCase(
            id="high-loan-claim",
            suite_id="default",
            severity="high",
            dialog=["Can you guarantee 90 percent loan?", "I need written confirmation"],
            expected_next_action="handover",
            expected_risk_level="risky",
            expected_lead_status="needs_human_review",
            expected_summary_fingerprint=summary_fingerprint(case_handover.summary),
            expected_output=case_handover,
            tags=("guardrail", "claim_control"),
        ),
        GoldenCase(
            id="medium-callback",
            suite_id="default",
            severity="medium",
            dialog=["I am busy now", "Call me tomorrow evening"],
            expected_next_action="callback",
            expected_risk_level="safe",
            expected_lead_status="warm",
            expected_summary_fingerprint=summary_fingerprint(case_callback.summary),
            expected_output=case_callback,
            tags=("callback",),
        ),
    ):
        store.add_case(case)

    review_output = BrainOutput(
        reply="Possession date please verify with our team.",
        next_action="handover",
        risk_level="risky",
        lead_status="needs_human_review",
        summary="Possession claim was not supported by KB",
        confidence=0.57,
    )
    review = HumanReviewItem(
        id="review-001",
        tenant_id="tenant-north",
        session_id="sess-551",
        reason="hallucination flagged",
        transcript=["Customer asked for possession date", "AI claimed December possession"],
        kb_chunks=["RERA possession date: please verify with sales office."],
        brain_json=review_output,
        guardrail_decision="claim_rewritten",
        prompt_version="prompt_v14",
        model_version="groq_llama",
        retrieved_score=0.61,
    )
    store.enqueue_review(review)
    store.save_incident(
        HallucinationIncident(
            id="hall-001",
            queue_item_id=review.id,
            prompt_version=review.prompt_version,
            model_version=review.model_version,
            kb_chunks=review.kb_chunks,
            retrieved_score=review.retrieved_score,
            claim="AI claimed December possession without KB support",
        )
    )
    return store
