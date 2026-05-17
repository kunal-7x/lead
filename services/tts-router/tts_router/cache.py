from __future__ import annotations

import hashlib
import time
from typing import Protocol


def cache_key(text: str, voice_id: str, lang: str) -> str:
    """sha256(text + voice_id + lang) → hex digest used as cache key."""
    raw = f"{text}|{voice_id}|{lang}".encode()
    return hashlib.sha256(raw).hexdigest()


class AudioCache(Protocol):
    async def get(self, key: str) -> bytes | None: ...
    async def put(self, key: str, audio: bytes) -> None: ...


class RedisAudioCache:
    """Stores audio clips in Redis with a 7-day TTL.

    Production: use a real redis.asyncio.Redis client.
    Tests: use FakeAudioCache.
    """
    TTL = 7 * 24 * 3600  # 7 days

    def __init__(self, redis) -> None:
        self._redis = redis

    async def get(self, key: str) -> bytes | None:
        return await self._redis.get(f"tts:clip:{key}")

    async def put(self, key: str, audio: bytes) -> None:
        await self._redis.setex(f"tts:clip:{key}", self.TTL, audio)


class FakeAudioCache:
    """In-memory cache for tests. Tracks timing to verify cache-hit latency."""

    def __init__(self) -> None:
        self._store: dict[str, bytes] = {}
        self.hit_count = 0
        self.miss_count = 0

    async def get(self, key: str) -> bytes | None:
        val = self._store.get(key)
        if val is not None:
            self.hit_count += 1
        else:
            self.miss_count += 1
        return val

    async def put(self, key: str, audio: bytes) -> None:
        self._store[key] = audio

    def size(self) -> int:
        return len(self._store)
