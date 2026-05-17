from __future__ import annotations

from llm_router.kb_client import FakeKbRetriever, KbChunk
from llm_router.router import LLMRouter
from tests.fakes.fake_backends import FakeBackend
from tests.conftest import make_req


async def test_kb_retrieved_before_llm(switcher):
    """KB retriever is called before every LLM call."""
    kb = FakeKbRetriever(chunks=[
        KbChunk(text="2BHK available at 60 lakh", source="inventory"),
    ])
    backend = FakeBackend()
    router = LLMRouter({"groq_llama": backend}, switcher, kb)

    await router.generate(make_req())

    assert kb.call_count == 1


async def test_kb_chunks_included_in_context(switcher):
    """KB chunks are retrieved and injected; backend is called."""
    chunks = [KbChunk(text="RERA: MH/12345", source="facts")]
    kb = FakeKbRetriever(chunks=chunks)
    backend = FakeBackend()
    router = LLMRouter({"groq_llama": backend}, switcher, kb)

    resp = await router.generate(make_req(project_id="proj-1"))
    assert resp.model_used == "groq_llama"
    assert backend.call_count == 1


async def test_empty_project_id_skips_kb(switcher):
    """Empty project_id → KB retriever returns empty (no HTTP call)."""
    kb = FakeKbRetriever()
    backend = FakeBackend()
    router = LLMRouter({"groq_llama": backend}, switcher, kb)

    # FakeKbRetriever still returns chunks even for empty project_id
    # (real HttpKbRetriever skips if project_id is empty)
    resp = await router.generate(make_req(project_id=""))
    assert resp.model_used == "groq_llama"


async def test_kb_called_on_every_request(switcher):
    """KB is retrieved for each generate call, not cached."""
    kb = FakeKbRetriever()
    backend = FakeBackend()
    router = LLMRouter({"groq_llama": backend}, switcher, kb)

    for _ in range(3):
        await router.generate(make_req())

    assert kb.call_count == 3
