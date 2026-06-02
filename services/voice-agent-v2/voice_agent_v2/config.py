"""Typed settings for the LiveKit + Pipecat voice agent.

All secrets/URLs are read from the environment (os.getenv ONLY).
No hardcoded secrets ever.

Adapted from services/voice-agent-worker/voice_agent/pipecat_runtime/config.py
with the transport changed from Vobiz/FastAPI WS to LiveKit.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field

from voice_agent_v2.models import SessionContext


def _bool_env(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in ("1", "true", "yes", "on")


def _float_env(name: str, default: float) -> float:
    try:
        return float(os.getenv(name, str(default)))
    except (TypeError, ValueError):
        return default


def _int_env(name: str, default: int) -> int:
    try:
        return int(os.getenv(name, str(default)))
    except (TypeError, ValueError):
        return default


@dataclass
class AgentSettings:
    """All runtime configuration, resolved from env at process start.

    Construct via from_env(). Secrets are pulled from env by name and held
    in-process only for the lifetime of the worker; they are never logged.
    """

    # ── LiveKit connection ──────────────────────────────────────────────────────
    livekit_url: str = "ws://localhost:7880"
    livekit_api_key: str = ""
    livekit_api_secret: str = ""
    livekit_room_name: str = "agent-room"

    # ── STT (Sarvam) ─────────────────────────────────────────────────────────────
    sarvam_api_key: str = ""
    sarvam_stt_model: str = "saaras:v3"
    sarvam_stt_mode: str = "codemix"   # codemix = Hinglish/code-switch mode

    # ── TTS ─────────────────────────────────────────────────────────────────────
    elevenlabs_api_key: str = ""
    elevenlabs_voice_id: str = ""
    elevenlabs_model: str = "eleven_flash_v2_5"

    # ── Voice / prosody defaults ─────────────────────────────────────────────────
    sarvam_tts_speaker: str = "rahul"
    tts_premium: bool = False

    # ── LLM router (for next unit) ───────────────────────────────────────────────
    llm_router_url: str = "http://llm-router:8111"
    guardrail_url: str = "http://guardrail:8112"

    # ── Infra ───────────────────────────────────────────────────────────────────
    redis_url: str = "redis://localhost:6379"
    nats_url: str = "nats://localhost:4222"
    event_publisher: str = "demo"   # demo | nats

    # ── Audio ──────────────────────────────────────────────────────────────────
    audio_in_sample_rate: int = 16000    # LiveKit default
    audio_out_sample_rate: int = 16000   # LiveKit default (ElevenLabs works at 16k)

    extra: dict = field(default_factory=dict)

    @classmethod
    def from_env(cls) -> "AgentSettings":
        return cls(
            livekit_url=os.getenv("LIVEKIT_URL", "ws://localhost:7880"),
            livekit_api_key=os.getenv("LIVEKIT_API_KEY", ""),
            livekit_api_secret=os.getenv("LIVEKIT_API_SECRET", ""),
            livekit_room_name=os.getenv("LIVEKIT_ROOM_NAME", "agent-room"),
            sarvam_api_key=os.getenv("SARVAM_API_KEY", ""),
            sarvam_stt_model=os.getenv("SARVAM_STT_MODEL", "saaras:v3"),
            sarvam_stt_mode=os.getenv("SARVAM_STT_MODE", "codemix"),
            elevenlabs_api_key=os.getenv("ELEVENLABS_API_KEY", ""),
            elevenlabs_voice_id=os.getenv("ELEVENLABS_VOICE_ID", ""),
            elevenlabs_model=os.getenv("ELEVENLABS_MODEL", "eleven_flash_v2_5"),
            sarvam_tts_speaker=os.getenv("SARVAM_TTS_SPEAKER", "rahul"),
            tts_premium=_bool_env("TTS_PREMIUM", False),
            llm_router_url=os.getenv("LLM_ROUTER_URL", "http://llm-router:8111"),
            guardrail_url=os.getenv("GUARDRAIL_URL", "http://guardrail:8112"),
            redis_url=os.getenv("REDIS_URL", "redis://localhost:6379"),
            nats_url=os.getenv("NATS_URL", "nats://localhost:4222"),
            event_publisher=os.getenv("EVENT_PUBLISHER", "demo").strip().lower(),
            audio_in_sample_rate=_int_env("AUDIO_IN_SAMPLE_RATE", 16000),
            audio_out_sample_rate=_int_env("AUDIO_OUT_SAMPLE_RATE", 16000),
        )


def select_tts_provider(ctx: SessionContext, settings: AgentSettings) -> str:
    """Return "elevenlabs" or "sarvam" for THIS call.

    ElevenLabs when ctx.tts_premium OR global TTS_PREMIUM=1 OR
    a campaign flag (ctx.campaign_context["tts_premium"]);
    otherwise Sarvam bulbul:v3.

    Ported from voice-agent-worker/pipecat_runtime/config.py.
    """
    campaign_premium = False
    if isinstance(ctx.campaign_context, dict):
        campaign_premium = bool(ctx.campaign_context.get("tts_premium", False))
    if ctx.tts_premium or settings.tts_premium or campaign_premium:
        return "elevenlabs"
    return "sarvam"
