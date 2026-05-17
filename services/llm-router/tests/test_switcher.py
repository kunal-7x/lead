from __future__ import annotations

import pytest
import fakeredis.aioredis

from llm_router.switcher import ModelSwitcher, DEFAULT_MODEL, _GLOBAL_KEY
from llm_router.router import LLMRouter
from llm_router.kb_client import FakeKbRetriever
from tests.fakes.fake_backends import FakeBackend
from tests.conftest import make_req


async def test_default_model_is_groq(switcher):
    active = await switcher.active_model("tenant-1")
    assert active == DEFAULT_MODEL == "groq_llama"


async def test_global_model_switch(fake_redis):
    """Redis key llm:global_model=mistral_7b → routes to mistral fake."""
    await fake_redis.set(_GLOBAL_KEY, "mistral_7b")
    switcher = ModelSwitcher(fake_redis)

    mistral = FakeBackend(name="mistral_7b")
    groq = FakeBackend(name="groq_llama")
    router = LLMRouter({"mistral_7b": mistral, "groq_llama": groq}, switcher, FakeKbRetriever())

    resp = await router.generate(make_req(tenant_id="any-tenant"))
    assert resp.model_used == "mistral_7b"
    assert mistral.call_count == 1
    assert groq.call_count == 0


async def test_per_tenant_override(fake_redis):
    """Tenant A uses groq (global), tenant B has openai override."""
    await fake_redis.set("llm:tenant:tenant-b:model", "openai_gpt4o")
    switcher = ModelSwitcher(fake_redis)

    groq = FakeBackend(name="groq_llama")
    openai = FakeBackend(name="openai_gpt4o")
    router = LLMRouter({"groq_llama": groq, "openai_gpt4o": openai}, switcher, FakeKbRetriever())

    # tenant-a: no override → global default (groq)
    resp_a = await router.generate(make_req(tenant_id="tenant-a"))
    assert resp_a.model_used == "groq_llama"

    # tenant-b: has override → openai
    resp_b = await router.generate(make_req(tenant_id="tenant-b"))
    assert resp_b.model_used == "openai_gpt4o"


async def test_invalid_redis_value_falls_back_to_default(fake_redis):
    await fake_redis.set(_GLOBAL_KEY, "nonexistent_model")
    switcher = ModelSwitcher(fake_redis)
    active = await switcher.active_model("t1")
    assert active == DEFAULT_MODEL


async def test_set_invalid_model_raises(switcher):
    with pytest.raises(ValueError, match="Invalid model"):
        await switcher.set_global_model("bogus_model")


async def test_clear_tenant_override(fake_redis):
    await fake_redis.set("llm:tenant:t1:model", "mistral_7b")
    switcher = ModelSwitcher(fake_redis)
    assert await switcher.active_model("t1") == "mistral_7b"
    await switcher.clear_tenant_override("t1")
    assert await switcher.active_model("t1") == DEFAULT_MODEL
