from __future__ import annotations

from llm_router.router import LLMRouter, _build_chain
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


def test_build_chain_preferred_order():
    """groq_llama primary → groq_instant → cerebras_llama → openrouter fallback."""
    available = ["groq_llama", "groq_instant", "cerebras_llama", "openrouter",
                 "sarvam_llm", "openai_gpt4o"]
    chain = _build_chain("groq_llama", available)
    assert chain[0] == "groq_llama"
    assert chain[1] == "groq_instant"
    assert chain[2] == "cerebras_llama"
    assert chain[3] == "openrouter"


def test_build_chain_429_active_is_groq_instant():
    """When LLM_ACTIVE_MODEL=groq_instant, groq_llama drops behind it."""
    available = ["groq_llama", "groq_instant", "cerebras_llama", "openrouter"]
    chain = _build_chain("groq_instant", available)
    assert chain[0] == "groq_instant"
    # groq_llama should still appear (just not first)
    assert "groq_llama" in chain
    # cerebras_llama comes after groq_llama in preferred order
    assert chain.index("groq_llama") < chain.index("cerebras_llama")


async def test_groq_llama_429_falls_to_groq_instant(switcher):
    """Simulate groq_llama 429 → chain falls to groq_instant (not cerebras batch)."""
    groq_versatile = FakeBackend(name="groq_llama", raise_error=True)
    groq_instant = FakeBackend(name="groq_instant")
    cerebras = FakeBackend(name="cerebras_llama")
    router = LLMRouter(
        {"groq_llama": groq_versatile, "groq_instant": groq_instant, "cerebras_llama": cerebras},
        switcher,
        FakeKbRetriever(),
    )
    resp = await router.generate(make_req())
    assert resp.model_used == "groq_instant"
    assert groq_versatile.call_count == 1
    assert groq_instant.call_count == 1
    assert cerebras.call_count == 0  # not reached


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
