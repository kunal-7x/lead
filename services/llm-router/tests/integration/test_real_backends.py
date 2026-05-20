"""Live integration tests — only run when RUN_LIVE_TESTS=1.

Cost guard: ≤50 input tokens per call.
Run: RUN_LIVE_TESTS=1 uv run pytest tests/integration -v
"""
from __future__ import annotations

import os

import pytest

pytestmark = pytest.mark.skipif(
    os.getenv("RUN_LIVE_TESTS") != "1",
    reason="set RUN_LIVE_TESTS=1 to run live provider tests",
)

_SIMPLE_REQ_KWARGS = {
    "user_turn": "In one word, what is the capital of India?",
    "lang": "en",
    "tenant_id": "live-test",
    "session_id": "live-sess",
    "project_id": "",
    # dialog_history empty, no KB context → minimal tokens
}


def _make_req():
    from llm_router.models import LLMRequest
    return LLMRequest(**_SIMPLE_REQ_KWARGS)


def _delhi_in(text: str) -> bool:
    return "delhi" in text.lower() or "दिल्ली" in text


# ── Groq Llama ───────────────────────────────────────────────────────────────

async def test_groq_llama_live() -> None:
    from llm_router.backends.groq import GroqLlamaBackend
    backend = GroqLlamaBackend()
    assert backend._api_key, "GROQ_API_KEY not set"
    brain, pt, ct = await backend.generate(_make_req(), "(no KB context)")
    assert isinstance(brain.reply, str) and brain.reply
    print(f"[groq_llama] reply={brain.reply!r} tokens={pt}+{ct}")


async def test_groq_health_live() -> None:
    from llm_router.backends.groq import GroqLlamaBackend
    backend = GroqLlamaBackend()
    ok = await backend.health_check()
    assert ok, "Groq health check failed — check GROQ_API_KEY"


# ── OpenRouter ───────────────────────────────────────────────────────────────

async def test_openrouter_live() -> None:
    from llm_router.backends.openai_backend import OpenRouterBackend
    backend = OpenRouterBackend()
    assert backend._api_key, "OPENROUTER_API_KEY not set"
    brain, pt, ct = await backend.generate(_make_req(), "(no KB context)")
    assert isinstance(brain.reply, str) and brain.reply
    print(f"[openrouter] reply={brain.reply!r} tokens={pt}+{ct}")


# ── Sarvam LLM ───────────────────────────────────────────────────────────────

async def test_sarvam_llm_live() -> None:
    from llm_router.backends.sarvam import SarvamLLMBackend
    backend = SarvamLLMBackend()
    assert backend._api_key, "SARVAM_API_KEY not set"
    brain, pt, ct = await backend.generate(_make_req(), "(no KB context)")
    assert isinstance(brain.reply, str) and brain.reply
    print(f"[sarvam_llm] reply={brain.reply!r} tokens={pt}+{ct}")


# ── Optional: OpenAI ─────────────────────────────────────────────────────────

async def test_openai_live() -> None:
    if not os.getenv("OPENAI_API_KEY"):
        pytest.skip("OPENAI_API_KEY not set")
    from llm_router.backends.openai_backend import OpenAIBackend
    backend = OpenAIBackend()
    brain, pt, ct = await backend.generate(_make_req(), "(no KB context)")
    assert isinstance(brain.reply, str) and brain.reply
    print(f"[openai] reply={brain.reply!r} tokens={pt}+{ct}")


# ── Optional: Anthropic ───────────────────────────────────────────────────────

async def test_anthropic_live() -> None:
    if not os.getenv("ANTHROPIC_API_KEY"):
        pytest.skip("ANTHROPIC_API_KEY not set")
    from llm_router.backends.openai_backend import AnthropicBackend
    backend = AnthropicBackend()
    brain, pt, ct = await backend.generate(_make_req(), "(no KB context)")
    assert isinstance(brain.reply, str) and brain.reply
    print(f"[anthropic] reply={brain.reply!r} tokens={pt}+{ct}")


# ── Optional: Gemini ──────────────────────────────────────────────────────────

async def test_gemini_live() -> None:
    if not os.getenv("GOOGLE_GEMINI_API_KEY"):
        pytest.skip("GOOGLE_GEMINI_API_KEY not set")
    from llm_router.backends.openai_backend import GoogleGeminiBackend
    import os as _os
    _os.environ.setdefault("GOOGLE_API_KEY", _os.getenv("GOOGLE_GEMINI_API_KEY", ""))
    backend = GoogleGeminiBackend(api_key=_os.getenv("GOOGLE_GEMINI_API_KEY", ""))
    brain, pt, ct = await backend.generate(_make_req(), "(no KB context)")
    assert isinstance(brain.reply, str) and brain.reply
    print(f"[gemini] reply={brain.reply!r}")
