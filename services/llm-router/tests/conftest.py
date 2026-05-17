from __future__ import annotations

import pytest
import fakeredis.aioredis

from llm_router.switcher import ModelSwitcher
from llm_router.kb_client import FakeKbRetriever


@pytest.fixture
async def fake_redis():
    return fakeredis.aioredis.FakeRedis()


@pytest.fixture
async def switcher(fake_redis):
    return ModelSwitcher(fake_redis)


@pytest.fixture
def fake_kb():
    return FakeKbRetriever()


def make_req(**kwargs):
    from llm_router.models import LLMRequest
    defaults = {
        "user_turn": "2BHK ka price kya hai?",
        "lang": "hi-en",
        "tenant_id": "tenant-1",
        "session_id": "sess-1",
        "project_id": "proj-1",
    }
    defaults.update(kwargs)
    return LLMRequest(**defaults)
