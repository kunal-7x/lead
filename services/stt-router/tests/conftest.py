from __future__ import annotations

import pytest
import fakeredis.aioredis

from stt_router.switcher import EngineSwitcher


@pytest.fixture
async def fake_redis():
    return fakeredis.aioredis.FakeRedis()


@pytest.fixture
async def switcher(fake_redis):
    return EngineSwitcher(fake_redis)
