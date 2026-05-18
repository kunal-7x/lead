from __future__ import annotations

from eval_harness.models import AIFeedback, BrainOutput, GoldenCase, summary_fingerprint
from eval_harness.store import InMemoryStore


class ReviewService:
    def __init__(self, store: InMemoryStore) -> None:
        self.store = store

    def accept_correction(
        self,
        queue_item_id: str,
        reviewer_id: str,
        corrected_output: BrainOutput,
        notes: str,
    ) -> AIFeedback:
        item = self.store.human_review_queue[queue_item_id]
        feedback = AIFeedback(
            id=f"feedback-{len(self.store.ai_feedback) + 1}",
            queue_item_id=queue_item_id,
            reviewer_id=reviewer_id,
            corrected_output=corrected_output,
            accepted=True,
            notes=notes,
        )
        self.store.save_feedback(feedback)
        self.store.update_review_status(queue_item_id, "accepted")
        self.store.add_case(
            GoldenCase(
                id=f"feedback-{queue_item_id}",
                suite_id="feedback",
                severity="high" if item.reason in {"hallucination flagged", "unsafe"} else "medium",
                dialog=item.transcript,
                expected_next_action=corrected_output.next_action,
                expected_risk_level=corrected_output.risk_level,
                expected_lead_status=corrected_output.lead_status,
                expected_summary_fingerprint=summary_fingerprint(corrected_output.summary),
                expected_output=corrected_output,
                tags=("review_feedback", item.reason.replace(" ", "_")),
            )
        )
        return feedback
