from __future__ import annotations

from pydantic import BaseModel, Field


class STTResult(BaseModel):
    text: str
    is_final: bool
    confidence: float = Field(ge=0.0, le=1.0)
    language: str          # "hi" | "en" | "hi-en"
    engine_used: str       # "sarvam" | "indicconformer" | "faster_whisper" | "groq_whisper"
    latency_ms: int
    turn_start_ts: float
    turn_end_ts: float | None = None


class STTRequest(BaseModel):
    lang: str = "hi-en"
    session_id: str
    tenant_id: str


class EngineHealth(BaseModel):
    name: str
    available: bool
    p95_latency_ms: int | None = None
    last_error: str | None = None


class HealthResponse(BaseModel):
    engines: list[EngineHealth]
