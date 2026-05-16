"""Redis client with per-tenant key namespacing."""
from __future__ import annotations

from datetime import timedelta

import redis.asyncio as aioredis


class TenantRedis:
    def __init__(self, client: aioredis.Redis, tenant_id: str) -> None:
        self._client = client
        self._tenant_id = tenant_id

    def _key(self, k: str) -> str:
        return f"t:{self._tenant_id}:{k}"

    async def set(self, key: str, value: str, ttl: timedelta | None = None) -> None:
        await self._client.set(self._key(key), value, ex=ttl)

    async def get(self, key: str) -> str | None:
        return await self._client.get(self._key(key))

    async def delete(self, *keys: str) -> None:
        await self._client.delete(*[self._key(k) for k in keys])


def create_client(url: str) -> aioredis.Redis:
    return aioredis.from_url(url, decode_responses=True)


def for_tenant(client: aioredis.Redis, tenant_id: str) -> TenantRedis:
    return TenantRedis(client, tenant_id)
