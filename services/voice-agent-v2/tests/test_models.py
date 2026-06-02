"""Tests for models and config modules."""

from __future__ import annotations

import os

from voice_agent_v2.config import AgentSettings, select_tts_provider
from voice_agent_v2.models import BrainOutput, SessionContext, load_session_context_from_dict


def test_session_context_defaults():
    ctx = SessionContext(session_id="s1", tenant_id="t1")
    assert ctx.session_id == "s1"
    assert ctx.tenant_id == "t1"
    assert ctx.lang == "hi-en"
    assert ctx.tts_premium is False


def test_load_session_context_from_dict_filters_unknown_keys():
    data = {
        "tenant_id": "t2",
        "campaign_id": "camp1",
        "unknown_future_field": "ignored",
    }
    ctx = load_session_context_from_dict("sess-1", data)
    assert ctx.session_id == "sess-1"
    assert ctx.tenant_id == "t2"
    assert ctx.campaign_id == "camp1"
    assert not hasattr(ctx, "unknown_future_field")


def test_load_session_context_tts_premium_override(monkeypatch):
    monkeypatch.setenv("TTS_PREMIUM", "1")
    ctx = load_session_context_from_dict("s", {"tenant_id": "t", "tts_premium": False})
    assert ctx.tts_premium is True


def test_select_tts_provider_default_is_sarvam():
    settings = AgentSettings()
    ctx = SessionContext(session_id="s", tenant_id="t", tts_premium=False)
    assert select_tts_provider(ctx, settings) == "sarvam"


def test_select_tts_provider_elevenlabs_when_ctx_premium():
    settings = AgentSettings()
    ctx = SessionContext(session_id="s", tenant_id="t", tts_premium=True)
    assert select_tts_provider(ctx, settings) == "elevenlabs"


def test_select_tts_provider_elevenlabs_when_settings_premium():
    settings = AgentSettings(tts_premium=True)
    ctx = SessionContext(session_id="s", tenant_id="t", tts_premium=False)
    assert select_tts_provider(ctx, settings) == "elevenlabs"


def test_select_tts_provider_elevenlabs_from_campaign():
    settings = AgentSettings()
    ctx = SessionContext(
        session_id="s", tenant_id="t",
        tts_premium=False,
        campaign_context={"tts_premium": True},
    )
    assert select_tts_provider(ctx, settings) == "elevenlabs"


def test_agent_settings_from_env(monkeypatch):
    monkeypatch.setenv("LIVEKIT_URL", "ws://custom:7880")
    monkeypatch.setenv("LIVEKIT_API_KEY", "mykey")
    settings = AgentSettings.from_env()
    assert settings.livekit_url == "ws://custom:7880"
    assert settings.livekit_api_key == "mykey"


def test_brain_output_defaults():
    brain = BrainOutput(reply="Hello")
    assert brain.lead_status == "warm"
    assert brain.should_handover_to_human is False
    assert brain.lead_score == 0
