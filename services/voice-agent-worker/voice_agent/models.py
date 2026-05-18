from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any
from pydantic import BaseModel


@dataclass
class SessionContext:
    session_id: str
    tenant_id: str
    campaign_id: str = ""
    kb_version_id: str = ""
    voice_profile_id: str = "meera"
    lang: str = "hi-en"
    system_prompt_version: str = "v1"
    lead_id: str = ""
    call_session_id: str = ""
    project_id: str = ""
    tts_premium: bool = False


@dataclass
class CallTurn:
    session_id: str
    turn_index: int
    speaker: str           # "agent" | "caller"
    transcript: str = ""
    confidence: float = 0.0
    stt_engine: str = ""
    reply: str = ""
    lead_status: str = ""
    next_action: str = ""
    brain_json: dict = field(default_factory=dict)
    tts_engine: str = ""
    cache_hit: bool = False


class STTResult(BaseModel):
    text: str
    confidence: float = 0.0
    engine_used: str = ""
    is_final: bool = True


class BrainOutput(BaseModel):
    reply: str
    lead_status: str = "warm"
    lead_score: int = 0
    next_action: str = "qualify"
    should_send_whatsapp: bool = False
    should_handover_to_human: bool = False
    should_create_site_visit: bool = False
    should_create_callback: bool = False
    risk_level: str = "safe"
    confidence: float = 0.8
    summary: str = ""


class TTSResult(BaseModel):
    audio: bytes
    tier_used: str = ""
    cache_hit: bool = False
    sample_rate: int = 8000
