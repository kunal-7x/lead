from __future__ import annotations

"""Tests that campaign_context flows into the system prompt."""

import pytest

from llm_router.backends.base import format_campaign_context
from llm_router.models import LLMRequest

_CTX = {
    "product_description": "Luxury 2BHK apartments in Gurugram sector 45",
    "offer": "Early-bird discount of 5 lakh for bookings before March 31",
    "talking_points": ["RERA registered", "Ready to move"],
    "do_not_say": ["competitor pricing", "guaranteed returns"],
    "goal": "Book a site visit",
}


# ── format_campaign_context helper ────────────────────────────────────────────

def test_format_campaign_context_contains_product():
    out = format_campaign_context(_CTX)
    assert "Luxury 2BHK apartments in Gurugram sector 45" in out


def test_format_campaign_context_contains_offer():
    out = format_campaign_context(_CTX)
    assert "Early-bird discount of 5 lakh" in out


def test_format_campaign_context_contains_talking_point():
    out = format_campaign_context(_CTX)
    assert "RERA registered" in out


def test_format_campaign_context_contains_do_not_say():
    out = format_campaign_context(_CTX)
    assert "competitor pricing" in out


def test_format_campaign_context_contains_goal():
    out = format_campaign_context(_CTX)
    assert "Book a site visit" in out


def test_format_campaign_context_empty_returns_empty():
    assert format_campaign_context({}) == ""
    assert format_campaign_context(None) == ""  # type: ignore[arg-type]


# ── System prompt assembly via _build_messages ────────────────────────────────

def test_build_messages_injects_campaign_context():
    """_build_messages must embed campaign_context text in the system message."""
    from llm_router.backends.groq import GroqLlamaBackend

    backend = GroqLlamaBackend(api_key="test-key")
    req = LLMRequest(
        user_turn="hello",
        lang="hi-en",
        tenant_id="t1",
        session_id="s1",
        campaign_context=_CTX,
    )
    messages = backend._build_messages(req, kb_context="")
    system_content = messages[0]["content"]

    assert "Luxury 2BHK apartments in Gurugram sector 45" in system_content
    assert "Early-bird discount of 5 lakh" in system_content
    assert "RERA registered" in system_content
    assert "competitor pricing" in system_content


def test_build_messages_no_campaign_context_no_block():
    """When campaign_context is absent, no campaign block appears."""
    from llm_router.backends.groq import GroqLlamaBackend

    backend = GroqLlamaBackend(api_key="test-key")
    req = LLMRequest(
        user_turn="hello",
        lang="hi-en",
        tenant_id="t1",
        session_id="s1",
    )
    messages = backend._build_messages(req, kb_context="")
    system_content = messages[0]["content"]
    assert "--- Campaign Context ---" not in system_content
