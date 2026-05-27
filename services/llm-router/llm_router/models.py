from __future__ import annotations

from typing import Any, Literal
from pydantic import BaseModel, Field, model_validator


class Budget(BaseModel):
    value: int | None = None
    text: str | None = None
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)


_NULLISH = {"null", "none", "nil", "n/a", "na", ""}


def _scrub_nullish(values: dict[str, Any]) -> dict[str, Any]:
    """LLMs sometimes emit "null"/"none" strings for Optional fields.
    Coerce those to actual None so Pydantic validation passes.
    """
    for k, v in list(values.items()):
        if isinstance(v, str) and v.strip().lower() in _NULLISH:
            values[k] = None
    return values


class BrainOutput(BaseModel):
    """Enforced output schema from the LLM. Never changed without migration."""

    @model_validator(mode="before")
    @classmethod
    def _coerce_nullish_strings(cls, data: Any) -> Any:
        if isinstance(data, dict):
            return _scrub_nullish(data)
        return data

    reply: str
    lead_status: Literal[
        "hot", "warm", "cold", "call_later", "not_interested",
        "wrong_number", "opt_out", "broker", "fake", "needs_human_review"
    ]
    lead_score: int = Field(ge=0, le=100)
    budget: Budget = Field(default_factory=Budget)
    property_type: str | None = None
    purpose: Literal["self_use", "investment"] | None = None
    timeline_days: int | None = None
    location_pref: str | None = None
    objection: str | None = None
    next_action: Literal[
        "qualify", "book_site_visit", "callback", "handover", "end_call", "opt_out"
    ]
    should_send_whatsapp: bool = False
    should_handover_to_human: bool = False
    should_create_site_visit: bool = False
    should_create_callback: bool = False
    risk_level: Literal["safe", "risky", "unsafe"] = "safe"
    confidence: float = Field(default=0.8, ge=0.0, le=1.0)
    summary: str


class KbChunk(BaseModel):
    text: str
    source: str = ""
    score: float = 1.0


class LLMRequest(BaseModel):
    system_prompt_version: str = "v1"
    kb_chunks: list[KbChunk] = Field(default_factory=list)
    dialog_history: list[dict] = Field(default_factory=list)
    user_turn: str
    lang: str = "hi-en"
    tenant_id: str
    session_id: str
    project_id: str = ""
    # Accumulated slot values from prior turns — injected into prompt to prevent re-asking.
    # Keys: budget_text, budget_value, location_pref, property_type, timeline_days, purpose.
    # Only non-null fields are included. None / omitted → no slots block in prompt.
    collected_slots: dict[str, Any] | None = None


class LLMResponse(BaseModel):
    brain: BrainOutput
    model_used: str
    prompt_tokens: int = 0
    completion_tokens: int = 0
    latency_ms: int = 0


class EngineHealth(BaseModel):
    name: str
    available: bool
    p95_latency_ms: int | None = None
    last_error: str | None = None


class HealthResponse(BaseModel):
    engines: list[EngineHealth]


FALLBACK_BRAIN = BrainOutput(
    reply="Hum aapki baat ek specialist ko transfer kar rahe hain.",
    lead_status="needs_human_review",
    lead_score=0,
    next_action="handover",
    should_handover_to_human=True,
    risk_level="risky",
    confidence=0.0,
    summary="LLM fallback — needs human review",
)
