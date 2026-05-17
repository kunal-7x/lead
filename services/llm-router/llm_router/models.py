from __future__ import annotations

from typing import Literal
from pydantic import BaseModel, Field


class Budget(BaseModel):
    value: int | None = None
    text: str | None = None
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)


class BrainOutput(BaseModel):
    """Enforced output schema from the LLM. Never changed without migration."""
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


class LLMResponse(BaseModel):
    brain: BrainOutput
    model_used: str
    prompt_tokens: int = 0
    completion_tokens: int = 0
    latency_ms: int = 0


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
