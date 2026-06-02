"""Unit tests for ProsodyShaper.

Ported from services/voice-agent-worker/tests/test_prosody.py (ProsodyShaper section only).
ProsodyShaper is a safety net — no connector injection.
"""

from __future__ import annotations

from voice_agent_v2.prosody import ProsodyShaper


def test_strips_markdown_bold():
    shaper = ProsodyShaper()
    assert shaper.shape("**Hello** world") == "Hello world"


def test_strips_markdown_italic():
    shaper = ProsodyShaper()
    assert shaper.shape("*Hello* world") == "Hello world"


def test_strips_heading_marker():
    shaper = ProsodyShaper()
    assert shaper.shape("## Hello world").strip() == "Hello world"


def test_strips_pipe():
    shaper = ProsodyShaper()
    result = shaper.shape("a | b | c")
    assert "|" not in result


def test_normalises_ellipsis():
    shaper = ProsodyShaper()
    result = shaper.shape("Hmm...")
    assert "..." not in result
    assert result  # not empty


def test_normalises_newlines():
    shaper = ProsodyShaper()
    result = shaper.shape("Line one\nLine two")
    assert "\n" not in result


def test_collapses_multispace():
    shaper = ProsodyShaper()
    result = shaper.shape("Hello   world")
    assert "  " not in result


def test_passthrough_clean_hindi():
    shaper = ProsodyShaper()
    text = "नमस्ते आप कैसे हैं?"
    result = shaper.shape(text)
    assert result == text


def test_reset_turn_no_crash():
    shaper = ProsodyShaper()
    shaper.reset_turn()
    shaper.reset_turn()


def test_backward_compat_allow_inject_param():
    """allow_inject kwarg must be accepted without crashing."""
    shaper = ProsodyShaper()
    result = shaper.shape("Hello world", allow_inject=False)
    assert result == "Hello world"


def test_backward_compat_constructor_params():
    """Old constructor params must be accepted without crashing."""
    shaper = ProsodyShaper(max_injections_per_turn=3, connector="तो")
    assert shaper.shape("test") == "test"
