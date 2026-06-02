"""Typed settings for the Pipecat runtime, read from env (os.getenv ONLY).

Every secret/URL is read by name from the environment — NOTHING is hardcoded.
Env var names reuse the existing worker's names so the box env (ALL_CREDENTIALS)
wires in unchanged; see PIPECAT_RUNTIME_DESIGN.md "Env vars" table.

Also provides the per-call TTS provider-selection helper (``select_tts_provider``)
which is independently unit-tested (no Pipecat import needed).
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field

# Reuse the existing SessionContext for typing the provider-selection helper.
from voice_agent.models import SessionContext


def _bool_env(name: str, default: bool = False) -> bool:
    """Parse a truthy env var. Accepts 1/true/yes/on (case-insensitive)."""
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
class PipecatSettings:
    """All runtime configuration, resolved from env at process start.

    Construct via :meth:`from_env`. Secrets are pulled from env by name and held
    in-process only for the lifetime of the worker; they are never logged.
    """

    # ── Service URLs (brain + fallbacks; reuse existing names) ────────────────
    llm_router_url: str = "http://llm-router:8111"
    stt_router_url: str = "http://stt-router:8110"   # fallback only
    tts_router_url: str = "http://tts-router:8113"   # NOT used for TTS now
    guardrail_url: str = "http://guardrail:8112"
    redis_url: str = "redis://localhost:6379"
    nats_url: str = "nats://localhost:4222"
    event_publisher: str = "nats"                    # nats | demo

    # ── Direct provider keys for Pipecat's native STT/TTS (the hot path) ──────
    sarvam_api_key: str = ""
    elevenlabs_api_key: str = ""
    elevenlabs_voice_id: str = ""
    elevenlabs_model: str = "eleven_flash_v2_5"

    # ── Voice / prosody defaults ──────────────────────────────────────────────
    sarvam_tts_speaker: str = "rahul"
    sarvam_stt_model: str = "saaras:v3"
    sarvam_tts_model: str = "bulbul:v3"
    tts_premium: bool = False
    tts_pace: float = 1.0
    tts_temperature: float = 0.6

    # ── CSO / brain knobs ──────────────────────────────────────────────────────
    groq_api_key: str = ""
    groq_api_key_2: str = ""
    cso_model: str = "llama-3.1-8b-instant"
    cso_timeout_s: float = 1.5
    cso_enabled: bool = True
    llm_history_max_msgs: int = 12
    low_conf_barge_in_threshold: float = 0.85

    # ── Server / runtime flag ──────────────────────────────────────────────────
    pipecat_port: int = 8140
    pipecat_host: str = "127.0.0.1"
    voice_runtime: str = "old"                       # old | pipecat
    public_ws_base: str = ""                         # wss host for the Stream XML
    session_timeout_s: int = 1800

    # Target output rate for the Vobiz serializer (µ-law 8 kHz).
    output_sample_rate: int = 8000
    extra: dict = field(default_factory=dict)

    @classmethod
    def from_env(cls) -> "PipecatSettings":
        return cls(
            llm_router_url=os.getenv("LLM_ROUTER_URL", "http://llm-router:8111"),
            stt_router_url=os.getenv("STT_ROUTER_URL", "http://stt-router:8110"),
            tts_router_url=os.getenv("TTS_ROUTER_URL", "http://tts-router:8113"),
            guardrail_url=os.getenv("GUARDRAIL_URL", "http://guardrail:8112"),
            redis_url=os.getenv("REDIS_URL", "redis://localhost:6379"),
            nats_url=os.getenv("NATS_URL", "nats://localhost:4222"),
            event_publisher=os.getenv("EVENT_PUBLISHER", "nats").strip().lower(),
            sarvam_api_key=os.getenv("SARVAM_API_KEY", ""),
            elevenlabs_api_key=os.getenv("ELEVENLABS_API_KEY", ""),
            elevenlabs_voice_id=os.getenv("ELEVENLABS_VOICE_ID", ""),
            elevenlabs_model=os.getenv("ELEVENLABS_MODEL", "eleven_flash_v2_5"),
            sarvam_tts_speaker=os.getenv("SARVAM_TTS_SPEAKER", "rahul"),
            sarvam_stt_model=os.getenv("SARVAM_STT_MODEL", "saaras:v3"),
            sarvam_tts_model=os.getenv("SARVAM_TTS_MODEL", "bulbul:v3"),
            tts_premium=_bool_env("TTS_PREMIUM", False),
            tts_pace=_float_env("TTS_PACE", 1.0),
            tts_temperature=_float_env("TTS_TEMPERATURE", 0.6),
            groq_api_key=os.getenv("GROQ_API_KEY", ""),
            groq_api_key_2=os.getenv("GROQ_API_KEY_2", ""),
            cso_model=os.getenv("CSO_MODEL", "llama-3.1-8b-instant"),
            cso_timeout_s=_float_env("CSO_TIMEOUT_S", 1.5),
            cso_enabled=_bool_env("CSO_ENABLED", True),
            llm_history_max_msgs=_int_env("LLM_HISTORY_MAX_MSGS", 12),
            low_conf_barge_in_threshold=_float_env("LOW_CONF_BARGE_IN_THRESHOLD", 0.85),
            pipecat_port=_int_env("PIPECAT_PORT", 8140),
            pipecat_host=os.getenv("PIPECAT_HOST", "127.0.0.1"),
            voice_runtime=os.getenv("VOICE_RUNTIME", "old").strip().lower(),
            public_ws_base=os.getenv("PUBLIC_WS_BASE", ""),
            session_timeout_s=_int_env("PIPECAT_SESSION_TIMEOUT_S", 1800),
            output_sample_rate=_int_env("PIPECAT_OUTPUT_SAMPLE_RATE", 8000),
        )

    @property
    def use_pipecat(self) -> bool:
        """True when the feature flag selects this runtime."""
        return self.voice_runtime == "pipecat"


# ── Provider selection (per call) ──────────────────────────────────────────────

def select_tts_provider(ctx: SessionContext, settings: PipecatSettings) -> str:
    """Return ``"elevenlabs"`` or ``"sarvam"`` for THIS call.

    ElevenLabs when ``ctx.tts_premium`` OR global ``TTS_PREMIUM=1`` OR a campaign
    flag (``ctx.campaign_context["tts_premium"]``); otherwise Sarvam bulbul:v3.
    Both providers stay available + switchable (founder requirement).
    """
    campaign_premium = False
    if isinstance(ctx.campaign_context, dict):
        campaign_premium = bool(ctx.campaign_context.get("tts_premium", False))
    if ctx.tts_premium or settings.tts_premium or campaign_premium:
        return "elevenlabs"
    return "sarvam"
