from __future__ import annotations

import json
import httpx
import pytest

from llm_router.backends.groq import GroqLlamaBackend
from llm_router.backends.vllm import VLLMBackend
from llm_router.backends.openai_backend import (
    OpenAIBackend, AnthropicBackend, OpenRouterBackend, GoogleGeminiBackend
)
from llm_router.models import LLMRequest

_BRAIN_JSON = json.dumps({
    "reply": "2BHK available hai.",
    "lead_status": "warm",
    "lead_score": 65,
    "budget": {"value": 6000000, "text": "60 lakh", "confidence": 0.9},
    "property_type": "2BHK",
    "purpose": "self_use",
    "timeline_days": 30,
    "location_pref": "Pune",
    "objection": None,
    "next_action": "qualify",
    "should_send_whatsapp": False,
    "should_handover_to_human": False,
    "should_create_site_visit": False,
    "should_create_callback": False,
    "risk_level": "safe",
    "confidence": 0.88,
    "summary": "Warm lead, 2BHK Pune",
})

_OPENAI_RESP = {
    "choices": [{"message": {"content": _BRAIN_JSON}}],
    "usage": {"prompt_tokens": 100, "completion_tokens": 50},
}

_ANTHROPIC_RESP = {
    "content": [{"text": _BRAIN_JSON}],
    "usage": {"input_tokens": 100, "output_tokens": 50},
}

_GEMINI_RESP = {
    "candidates": [{"content": {"parts": [{"text": _BRAIN_JSON}]}}]
}

_REQ = LLMRequest(user_turn="test", lang="hi-en", tenant_id="t1", session_id="s1")


def _mock(resp_json: dict, status: int = 200):
    return httpx.MockTransport(
        handler=lambda r: httpx.Response(status, json=resp_json)
    )


# ── Groq ──────────────────────────────────────────────────────────────────────

async def test_groq_generate_mocked():
    backend = GroqLlamaBackend(api_key="test-key")
    backend._client = httpx.AsyncClient(transport=_mock(_OPENAI_RESP))
    brain, pt, ct = await backend.generate(_REQ, "kb context")
    assert brain.lead_status == "warm"
    assert pt == 100 and ct == 50


async def test_groq_health_no_key():
    backend = GroqLlamaBackend(api_key="")
    assert await backend.health_check() is False


async def test_groq_health_mocked():
    backend = GroqLlamaBackend(api_key="test-key")
    backend._client = httpx.AsyncClient(transport=_mock({"object": "list"}, 200))
    assert await backend.health_check() is True


# ── vLLM ──────────────────────────────────────────────────────────────────────

async def test_vllm_generate_mocked():
    backend = VLLMBackend("qwen3_32b")
    backend._client = httpx.AsyncClient(transport=_mock(_OPENAI_RESP))
    brain, pt, ct = await backend.generate(_REQ, "kb")
    assert brain.confidence == 0.88


async def test_vllm_health_unreachable():
    backend = VLLMBackend(base_url="http://127.0.0.1:19999")
    assert await backend.health_check() is False


async def test_vllm_health_mocked():
    backend = VLLMBackend()
    backend._client = httpx.AsyncClient(transport=_mock({}, 200))
    assert await backend.health_check() is True


# ── OpenAI ────────────────────────────────────────────────────────────────────

async def test_openai_generate_mocked():
    backend = OpenAIBackend(api_key="test-key")
    backend._client = httpx.AsyncClient(transport=_mock(_OPENAI_RESP))
    brain, pt, ct = await backend.generate(_REQ, "kb")
    assert brain.next_action == "qualify"


async def test_openai_health_with_key():
    assert await OpenAIBackend(api_key="key").health_check() is True


async def test_openai_health_no_key():
    assert await OpenAIBackend(api_key="").health_check() is False


# ── Anthropic ─────────────────────────────────────────────────────────────────

async def test_anthropic_generate_mocked():
    backend = AnthropicBackend(api_key="test-key")
    backend._client = httpx.AsyncClient(transport=_mock(_ANTHROPIC_RESP))
    brain, pt, ct = await backend.generate(_REQ, "kb")
    assert brain.lead_score == 65


async def test_anthropic_health_with_key():
    assert await AnthropicBackend(api_key="key").health_check() is True


# ── OpenRouter ────────────────────────────────────────────────────────────────

async def test_openrouter_generate_mocked():
    backend = OpenRouterBackend(api_key="test-key")
    backend._client = httpx.AsyncClient(transport=_mock(_OPENAI_RESP))
    brain, _, _ = await backend.generate(_REQ, "kb")
    assert brain.risk_level == "safe"


# ── Google Gemini ─────────────────────────────────────────────────────────────

async def test_gemini_generate_mocked():
    backend = GoogleGeminiBackend(api_key="test-key")
    backend._client = httpx.AsyncClient(transport=_mock(_GEMINI_RESP))
    brain, _, _ = await backend.generate(_REQ, "kb")
    assert brain.summary == "Warm lead, 2BHK Pune"


async def test_gemini_health_with_key():
    assert await GoogleGeminiBackend(api_key="key").health_check() is True


# ── Base ─────────────────────────────────────────────────────────────────────

def test_build_messages_includes_system_and_user():
    from llm_router.backends.base import LLMBackend, BRAIN_SCHEMA_PROMPT
    from llm_router.backends.groq import GroqLlamaBackend
    backend = GroqLlamaBackend(api_key="x")
    req = LLMRequest(
        user_turn="price kya hai",
        lang="hi-en",
        tenant_id="t1",
        session_id="s1",
        dialog_history=[{"role": "user", "content": "prev msg"}],
    )
    msgs = backend._build_messages(req, "kb text")
    assert msgs[0]["role"] == "system"
    assert "kb text" in msgs[0]["content"]
    assert msgs[-1]["role"] == "user"
    assert msgs[-1]["content"] == "price kya hai"
