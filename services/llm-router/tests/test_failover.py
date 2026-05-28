from __future__ import annotations

import json

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


async def test_stream_text_groq_llama_429_skips_to_groq_instant(switcher, monkeypatch):
    """groq_llama 429 on stream_text → groq_instant tried via streaming, NOT cerebras batch.

    Monkeypatches _llm_stream_text so groq_llama raises (simulating 429) and
    groq_instant succeeds. Asserts: groq_instant is tried; cerebras is never reached.
    """
    import llm_router.router as router_module

    called: list[str] = []

    async def fake_stream_text(backend, req, kb_context):
        called.append(backend.name)
        if backend.name == "groq_llama":
            raise RuntimeError("429 rate_limit_exceeded")
        # groq_instant succeeds
        yield f"data: {json.dumps({'token': 'नमस्ते', 'done': False})}\n\n"
        yield f"data: {json.dumps({'token': '', 'done': True})}\n\n"

    monkeypatch.setattr(router_module, "_llm_stream_text", fake_stream_text)

    groq_versatile = FakeBackend(name="groq_llama")
    groq_instant = FakeBackend(name="groq_instant")
    cerebras = FakeBackend(name="cerebras_llama")

    router = LLMRouter(
        {"groq_llama": groq_versatile, "groq_instant": groq_instant, "cerebras_llama": cerebras},
        switcher,
        FakeKbRetriever(),
    )

    tokens = []
    async for sse in router.generate_stream_text(make_req()):
        tokens.append(sse)

    # groq_llama was tried (and 429'd), groq_instant was tried next via streaming
    assert called[0] == "groq_llama"
    assert called[1] == "groq_instant"
    # cerebras was never reached (groq_instant succeeded)
    assert "cerebras_llama" not in called
    # SSE tokens include the reply token
    token_data = [json.loads(t[len("data: "):].strip()) for t in tokens if t.startswith("data:")]
    assert any(d.get("token") == "नमस्ते" for d in token_data)
    assert token_data[-1]["done"] is True
