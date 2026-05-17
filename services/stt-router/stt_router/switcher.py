from __future__ import annotations

import os
from typing import Any

REDIS_KEY = "stt:active_engine"
VALID_ENGINES = {"sarvam", "indicconformer", "faster_whisper", "groq_whisper"}
DEFAULT_ENGINE = "sarvam"


class EngineSwitcher:
    """Reads the active engine name from Redis key stt:active_engine.

    Production: inject a real redis.asyncio.Redis client.
    Tests: inject fakeredis.aioredis.FakeRedis.
    Changing the Redis key at runtime changes routing immediately.
    """

    def __init__(self, redis: Any) -> None:
        self._redis = redis

    async def active_engine(self) -> str:
        raw = await self._redis.get(REDIS_KEY)
        if raw is None:
            return DEFAULT_ENGINE
        name = raw.decode() if isinstance(raw, bytes) else str(raw)
        return name if name in VALID_ENGINES else DEFAULT_ENGINE

    async def set_engine(self, name: str) -> None:
        if name not in VALID_ENGINES:
            raise ValueError(f"Invalid engine: {name!r}. Valid: {VALID_ENGINES}")
        await self._redis.set(REDIS_KEY, name)
