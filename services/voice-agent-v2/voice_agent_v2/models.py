"""Domain models for voice-agent-v2.

Ported from services/voice-agent-worker/voice_agent/models.py.
SessionContext and BrainOutput are the live-call data shapes.
Redis key for session: call:session:{session_id}
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field
from typing import Any

from pydantic import BaseModel


def _default_voice_profile_id() -> str:
    """Default speaker for ALL synthesis (greeting + per-turn replies).

    Defaults to SARVAM_TTS_SPEAKER env (rahul = natural male bulbul:v3).
    Redis may override per-campaign.
    """
    return os.getenv("SARVAM_TTS_SPEAKER", "rahul")


@dataclass
class SessionContext:
    """Per-call session context, loaded from Redis call:session:{session_id}."""

    session_id: str
    tenant_id: str
    campaign_id: str = ""
    kb_version_id: str = ""
    # Single source of truth for the spoken voice across greeting + replies.
    voice_profile_id: str = field(default_factory=_default_voice_profile_id)
    lang: str = "hi-en"
    system_prompt_version: str = "v1"
    lead_id: str = ""
    call_session_id: str = ""
    project_id: str = ""
    tts_premium: bool = False
    campaign_context: dict = field(default_factory=dict)


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
    # Slot fields — passed through from llm-router for collected_slots accumulation.
    budget: dict | None = None
    location_pref: str | None = None
    property_type: str | None = None
    timeline_days: int | None = None
    purpose: str | None = None


class TTSResult(BaseModel):
    audio: bytes
    tier_used: str = ""
    cache_hit: bool = False
    sample_rate: int = 16000


def load_session_context_from_dict(session_id: str, data: dict) -> SessionContext:
    """Construct SessionContext from a Redis-loaded dict (call:session:{id} shape).

    Filters to only fields declared on the dataclass, so unknown Redis keys
    are silently ignored (forward-compat).
    """
    ctx = SessionContext(session_id=session_id, **{
        k: v for k, v in data.items()
        if k in SessionContext.__dataclass_fields__ and k != "session_id"
    })
    # Operational override: force premium TTS when the primary provider is down.
    if os.getenv("TTS_PREMIUM") == "1":
        ctx.tts_premium = True
    return ctx
