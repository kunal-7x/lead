from __future__ import annotations

from dataclasses import dataclass, field, replace
from datetime import datetime, timezone
from hashlib import sha256
from typing import Literal

Severity = Literal["low", "medium", "high"]


def utcnow() -> datetime:
    return datetime.now(timezone.utc)


def summary_fingerprint(summary: str) -> str:
    normalized = " ".join(summary.lower().strip().split())
    return sha256(normalized.encode("utf-8")).hexdigest()[:16]


@dataclass(frozen=True)
class BrainOutput:
    reply: str
    next_action: str
    risk_level: str
    lead_status: str
    summary: str
    confidence: float = 0.9

    def with_regression(self) -> "BrainOutput":
        return replace(
            self,
            next_action="end_call",
            risk_level="risky",
            lead_status="cold",
            summary="Reviewer follow up required",
            confidence=0.52,
        )


@dataclass(frozen=True)
class GoldenCase:
    id: str
    suite_id: str
    severity: Severity
    dialog: list[str]
    expected_next_action: str
    expected_risk_level: str
    expected_lead_status: str
    expected_summary_fingerprint: str
    expected_output: BrainOutput
    tags: tuple[str, ...] = field(default_factory=tuple)


@dataclass(frozen=True)
class EvaluationResult:
    id: str
    run_id: str
    case_id: str
    severity: Severity
    score: float
    failures: tuple[str, ...]
    output: BrainOutput
    created_at: datetime = field(default_factory=utcnow)


@dataclass(frozen=True)
class PromptTestRun:
    id: str
    prompt_version: str
    model_version: str
    suite_id: str
    aggregate_score: float
    high_severity_score: float
    green: bool
    case_scores: dict[str, float]
    created_at: datetime = field(default_factory=utcnow)


@dataclass(frozen=True)
class HumanReviewItem:
    id: str
    tenant_id: str
    session_id: str
    reason: str
    transcript: list[str]
    kb_chunks: list[str]
    brain_json: BrainOutput
    guardrail_decision: str
    prompt_version: str
    model_version: str
    retrieved_score: float
    status: str = "queued"


@dataclass(frozen=True)
class AIFeedback:
    id: str
    queue_item_id: str
    reviewer_id: str
    corrected_output: BrainOutput
    accepted: bool
    notes: str
    created_at: datetime = field(default_factory=utcnow)


@dataclass(frozen=True)
class HallucinationIncident:
    id: str
    queue_item_id: str
    prompt_version: str
    model_version: str
    kb_chunks: list[str]
    retrieved_score: float
    claim: str
    created_at: datetime = field(default_factory=utcnow)
