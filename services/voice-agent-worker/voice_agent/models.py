from __future__ import annotations

import os
from dataclasses import dataclass, field
from typing import Any
from pydantic import BaseModel


def _default_voice_profile_id() -> str:
    """Default speaker for ALL synthesis (greeting + per-turn replies).

    Both the greeting (vobiz_media.run_vobiz_bridge) and every reply chunk
    (AgentLoop) send ctx.voice_profile_id to the tts-router as the `voice_id`.
    To guarantee ONE consistent voice across both paths, default it to the
    same SARVAM_TTS_SPEAKER env the tts-router uses (rahul = natural male
    bulbul:v3). Redis may still override per-campaign. Previously this was
    hardcoded to the v2 female "anushka", which only sounded male by relying
    on the tts-router's "unknown voice → default speaker" fallback — a fragile
    coincidence that broke the moment the env/default diverged (the reported
    female-greeting / male-reply split).
    """
    return os.getenv("SARVAM_TTS_SPEAKER", "rahul")


@dataclass
class SessionContext:
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
    budget: dict | None = None        # {"value": int|None, "text": str|None, ...}
    location_pref: str | None = None
    property_type: str | None = None
    timeline_days: int | None = None
    purpose: str | None = None


class TTSResult(BaseModel):
    audio: bytes
    tier_used: str = ""
    cache_hit: bool = False
    sample_rate: int = 8000
