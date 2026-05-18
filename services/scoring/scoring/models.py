from __future__ import annotations

from typing import Literal
from pydantic import BaseModel

Temperature = Literal["super_hot", "hot", "warm", "cold", "bad"]
BuyerType = Literal["self_use", "investment", "broker", "fake"]

TEMP_LABELS: list[str] = ["bad", "cold", "warm", "hot", "super_hot"]
BUYER_LABELS: list[str] = ["self_use", "investment", "broker", "fake"]
TEMP_TO_SCORE: dict[str, int] = {
    "bad": 0, "cold": 25, "warm": 50, "hot": 75, "super_hot": 100
}

FEATURE_NAMES: list[str] = [
    "utterance_count",
    "said_not_interested",
    "showed_interest",
    "asked_price",
    "gave_contact",
    "should_handover",
    "should_create_site_visit",
    "call_duration_secs",
    "avg_confidence",
    "budget_mentioned",
]


class TurnInput(BaseModel):
    session_id: str
    lead_id: str
    tenant_id: str
    turn_index: int = 0
    said_not_interested: bool = False
    showed_interest: bool = False
    asked_price: bool = False
    gave_contact: bool = False
    should_handover: bool = False
    should_create_site_visit: bool = False
    budget_mentioned: bool = False
    confidence: float = 0.0


class CallInput(BaseModel):
    session_id: str
    lead_id: str
    tenant_id: str
    summary: str = ""
    utterance_count: int = 0
    call_duration_secs: float = 0.0
    avg_confidence: float = 0.0
    said_not_interested: bool = False
    showed_interest: bool = False
    asked_price: bool = False
    gave_contact: bool = False
    should_handover: bool = False
    should_create_site_visit: bool = False
    budget_mentioned: bool = False


class FeatureVector(BaseModel):
    utterance_count: float = 0.0
    said_not_interested: float = 0.0
    showed_interest: float = 0.0
    asked_price: float = 0.0
    gave_contact: float = 0.0
    should_handover: float = 0.0
    should_create_site_visit: float = 0.0
    call_duration_secs: float = 0.0
    avg_confidence: float = 0.0
    budget_mentioned: float = 0.0


class Attribution(BaseModel):
    feature: str
    importance: float


class ScoreResult(BaseModel):
    lead_id: str
    session_id: str
    temperature: Temperature
    buyer_type: BuyerType
    score: int
    attributions: list[Attribution]
    model_id: str
    features_hash: str


class LeadHistory(BaseModel):
    lead_id: str
    latest: ScoreResult | None
    history: list[ScoreResult]
