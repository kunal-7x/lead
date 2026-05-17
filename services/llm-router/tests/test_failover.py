from __future__ import annotations

from llm_router.router import LLMRouter
from llm_router.kb_client import FakeKbRetriever
from tests.fakes.fake_backends import FakeBackend, FakeVLLMBackend, FakeGroqBackend
from tests.conftest import make_req


async def test_vllm_down_falls_back_to_groq(switcher):
    """vLLM backend down → automatically falls back to groq_llama."""
    vllm = FakeVLLMBackend(model_key="qwen3_32b", raise_error=True)
    groq = FakeGroqBackend()

    router = LLMRouter(
        {"qwen3_32b": vllm, "groq_llama": groq},
        switcher,
        FakeKbRetriever(),
    )
    # Force active model to qwen3_32b
    await switcher.set_global_model("qwen3_32b")

    resp = await router.generate(make_req())
    assert resp.model_used == "groq_llama"
    assert vllm.call_count == 1
    assert groq.call_count == 1


async def test_primary_timeout_uses_next(switcher):
    """Primary times out → next backend called."""
    slow = FakeBackend(name="groq_llama", timeout=True)
    fast = FakeBackend(name="openai_gpt4o")

    router = LLMRouter(
        {"groq_llama": slow, "openai_gpt4o": fast},
        switcher,
        FakeKbRetriever(),
    )

    resp = await router.generate(make_req())
    assert resp.model_used == "openai_gpt4o"


async def test_all_fail_returns_fallback(switcher):
    """All backends raise error → safe FALLBACK_BRAIN returned."""
    router = LLMRouter(
        {"groq_llama": FakeBackend(raise_error=True),
         "openai_gpt4o": FakeBackend(name="openai_gpt4o", raise_error=True)},
        switcher,
        FakeKbRetriever(),
    )
    resp = await router.generate(make_req())
    assert resp.model_used == "fallback"
    assert resp.brain.should_handover_to_human is True
