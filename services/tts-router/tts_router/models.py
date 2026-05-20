from __future__ import annotations

from pydantic import BaseModel, Field


class TTSRequest(BaseModel):
    text: str
    lang: str = "hi-en"
    voice_id: str = "meera"
    stream: bool = False
    tenant_id: str
    session_id: str
    project_id: str = ""
    tts_premium: bool = False   # set true to allow ElevenLabs


class TTSResult(BaseModel):
    audio: bytes                # L16 PCM 8kHz
    tier_used: str              # "cache" | "sarvam_bulbul" | "kokoro" | ...
    latency_ms: int
    cache_hit: bool = False
    sample_rate: int = 8000


class VoiceInfo(BaseModel):
    id: str
    name: str
    lang: str
    engine: str


class WarmRequest(BaseModel):
    phrases: list[str]
    lang: str = "hi-en"
    voice_id: str = "meera"
    tenant_id: str
    project_id: str = ""


class EngineHealth(BaseModel):
    name: str
    available: bool
    p95_latency_ms: int | None = None
    last_error: str | None = None


class HealthResponse(BaseModel):
    engines: list[EngineHealth]
