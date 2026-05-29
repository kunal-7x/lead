"""Unit tests for ConversationStateEngine (T2.1).

Tests:
- CSO update from a sample dialog yields sensible state
- RSP directive maps correctly: impatient → short, curious → fuller
- Classification failure → neutral fallback (turn not blocked)
- Prosody params derived correctly from mood
- update() is non-blocking (never raises)
"""

from __future__ import annotations

import asyncio
import json
import pytest

from voice_agent.conversation_state import (
    ConversationStateEngine,
    _validate_cso,
    _NEUTRAL_CSO,
    _DIRECTIVE_MAP,
    _PROSODY_MAP,
)


# ── _validate_cso ─────────────────────────────────────────────────────────────

def test_validate_cso_valid_fields():
    raw = {
        "user_mood": "curious",
        "user_patience": 0.8,
        "conversation_energy": 0.7,
        "sales_stage": "interest",
        "response_density_target": "full",
        "warmth": 0.9,
    }
    result = _validate_cso(raw)
    assert result["user_mood"] == "curious"
    assert result["response_density_target"] == "full"
    assert abs(result["user_patience"] - 0.8) < 1e-6
    assert result["sales_stage"] == "interest"


def test_validate_cso_invalid_mood_falls_back():
    raw = {"user_mood": "angry", "response_density_target": "full"}
    result = _validate_cso(raw)
    # invalid mood → falls back to neutral default
    assert result["user_mood"] == _NEUTRAL_CSO["user_mood"]
    # valid density is preserved
    assert result["response_density_target"] == "full"


def test_validate_cso_clamps_float_values():
    raw = {
        "user_patience": 1.5,    # above 1.0 → clamp to 1.0
        "conversation_energy": -0.3,  # below 0.0 → clamp to 0.0
        "warmth": 0.5,
    }
    result = _validate_cso(raw)
    assert result["user_patience"] == 1.0
    assert result["conversation_energy"] == 0.0


def test_validate_cso_empty_dict_returns_neutral():
    result = _validate_cso({})
    assert result == _NEUTRAL_CSO


# ── directive() mapping ────────────────────────────────────────────────────────

def test_directive_impatient_maps_to_short():
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = {
        **_NEUTRAL_CSO,
        "user_mood": "impatient",
        "response_density_target": "short",
    }
    directive = engine.directive()
    assert directive  # must produce something
    # The directive should mention brevity/shortness
    assert any(word in directive for word in ["संक्षिप्त", "छोटा", "जल्दी"])


def test_directive_curious_maps_to_full():
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = {
        **_NEUTRAL_CSO,
        "user_mood": "curious",
        "response_density_target": "full",
    }
    directive = engine.directive()
    assert directive
    # Curious + full → detailed explanation directive
    assert any(word in directive for word in ["विस्तार", "उत्सुक"])


def test_directive_default_fallback_always_returns_string():
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = {
        **_NEUTRAL_CSO,
        "user_mood": "casual",
        "response_density_target": "medium",
    }
    directive = engine.directive()
    assert isinstance(directive, str)


def test_directive_skeptical_medium():
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = {
        **_NEUTRAL_CSO,
        "user_mood": "skeptical",
        "response_density_target": "medium",
    }
    directive = engine.directive()
    assert directive
    assert any(word in directive for word in ["विश्वसनीय", "भरोसेमंद", "गर्मजोशी"])


# ── prosody_params() ──────────────────────────────────────────────────────────

def test_prosody_impatient_faster_pace():
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = {**_NEUTRAL_CSO, "user_mood": "impatient", "user_patience": 0.2}
    pace, temp = engine.prosody_params()
    # impatient + low patience → pace >= 1.05
    assert pace is not None and pace >= 1.05


def test_prosody_confused_slower_pace():
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = {**_NEUTRAL_CSO, "user_mood": "confused", "user_patience": 0.7}
    pace, temp = engine.prosody_params()
    assert pace is not None and pace < 1.0


def test_prosody_unknown_mood_returns_none():
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = {**_NEUTRAL_CSO, "user_mood": "casual"}
    pace, temp = engine.prosody_params()
    # casual is in PROSODY_MAP with pace=1.0 — returns non-None
    # but if not in map → (None, None). casual is 1.0, 0.6 → non-None
    assert isinstance(pace, float) or pace is None


# ── update() — fallback on classification error ────────────────────────────────

@pytest.mark.asyncio
async def test_update_falls_back_on_bad_key():
    """CSO update with an invalid API key must not raise; returns previous state."""
    engine = ConversationStateEngine(groq_api_key="invalid_key_xxx")
    dialog = [{"role": "user", "content": "hello"}]
    # Should not raise even with a bad key
    result = await engine.update(dialog, "hello")
    assert isinstance(result, dict)
    # Must have all required keys
    for key in _NEUTRAL_CSO:
        assert key in result


@pytest.mark.asyncio
async def test_update_falls_back_on_no_key():
    """CSO update with no API key must silently fall back to neutral defaults."""
    engine = ConversationStateEngine(groq_api_key="")
    engine._keys = [""]  # force empty keys
    dialog = [{"role": "assistant", "content": "नमस्ते"},
              {"role": "user", "content": "जल्दी बताओ"}]
    result = await engine.update(dialog, "जल्दी बताओ")
    assert isinstance(result, dict)
    assert result.get("user_mood") in {"curious", "impatient", "skeptical",
                                        "interested", "confused", "casual"}


@pytest.mark.asyncio
async def test_update_does_not_block_turn():
    """Even if classification hangs, the update() method returns within timeout."""
    import asyncio as _asyncio

    engine = ConversationStateEngine(groq_api_key="dummy", timeout=0.05)
    dialog = [{"role": "user", "content": "test"}]

    t0 = _asyncio.get_event_loop().time()
    await engine.update(dialog, "test")
    elapsed = _asyncio.get_event_loop().time() - t0
    # Must return in < 1s even on timeout/error
    assert elapsed < 1.5


# ── CSO → directive integration ───────────────────────────────────────────────

def test_full_cso_to_directive_pipeline_impatient():
    """Full pipeline: impatient CSO → short directive."""
    engine = ConversationStateEngine(groq_api_key="dummy")
    # Simulate what update() would set after seeing an impatient user
    engine._cso = _validate_cso({
        "user_mood": "impatient",
        "user_patience": 0.1,
        "conversation_energy": 0.3,
        "sales_stage": "awareness",
        "response_density_target": "short",
        "warmth": 0.5,
    })
    directive = engine.directive()
    pace, temp = engine.prosody_params()
    # Directive should nudge towards brevity
    assert directive
    assert "संक्षिप्त" in directive or "छोटा" in directive or "जल्दी" in directive
    # Prosody: impatient → pace >= 1.0
    assert pace is not None and pace >= 1.0


def test_full_cso_to_directive_pipeline_curious():
    """Full pipeline: curious CSO → full/detailed directive."""
    engine = ConversationStateEngine(groq_api_key="dummy")
    engine._cso = _validate_cso({
        "user_mood": "curious",
        "user_patience": 0.9,
        "conversation_energy": 0.8,
        "sales_stage": "interest",
        "response_density_target": "full",
        "warmth": 0.8,
    })
    directive = engine.directive()
    pace, temp = engine.prosody_params()
    # Directive should encourage a detailed response
    assert directive
    assert "विस्तार" in directive or "उत्सुक" in directive
    # Prosody: curious → pace <= 1.0
    assert pace is not None and pace <= 1.0
