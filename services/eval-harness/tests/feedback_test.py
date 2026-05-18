from eval_harness.models import BrainOutput


def test_feedback_correction_writes_feedback_and_updates_corpus(review_service) -> None:
    corrected = BrainOutput(
        reply="Possession timeline sales team verify karegi.",
        next_action="handover",
        risk_level="risky",
        lead_status="needs_human_review",
        summary="Possession timeline must be verified by sales",
        confidence=0.93,
    )

    feedback = review_service.accept_correction(
        "review-001",
        reviewer_id="reviewer-7",
        corrected_output=corrected,
        notes="Removed unsupported possession date",
    )

    assert feedback.accepted is True
    assert review_service.store.human_review_queue["review-001"].status == "accepted"
    assert review_service.store.ai_feedback[0].reviewer_id == "reviewer-7"
    assert "feedback-review-001" in review_service.store.golden_test_cases
