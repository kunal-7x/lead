from __future__ import annotations

from typing import Any

VALID_ENGINES = {"sarvam_bulbul", "indic_parler", "indicf5", "kokoro", "elevenlabs"}
DEFAULT_ENGINE = "sarvam_bulbul"

_GLOBAL_KEY = "tts:global_engine"


def _tenant_key(tenant_id: str) -> str:
    return f"tts:tenant:{tenant_id}:engine"


class EngineSwitcher:
    """Reads active TTS engine from Redis (per-tenant override → global → default)."""

    def __init__(self, redis: Any) -> None:
        self._redis = redis

    async def active_engine(self, tenant_id: str) -> str:
        raw = await self._redis.get(_tenant_key(tenant_id))
        if raw is not None:
            name = raw.decode() if isinstance(raw, bytes) else str(raw)
            if name in VALID_ENGINES:
                return name
        raw = await self._redis.get(_GLOBAL_KEY)
        if raw is not None:
            name = raw.decode() if isinstance(raw, bytes) else str(raw)
            if name in VALID_ENGINES:
                return name
        return DEFAULT_ENGINE

    async def set_global_engine(self, engine: str) -> None:
        _validate(engine)
        await self._redis.set(_GLOBAL_KEY, engine)

    async def set_tenant_engine(self, tenant_id: str, engine: str) -> None:
        _validate(engine)
        await self._redis.set(_tenant_key(tenant_id), engine)


def _validate(engine: str) -> None:
    if engine not in VALID_ENGINES:
        raise ValueError(f"Invalid engine: {engine!r}. Valid: {VALID_ENGINES}")
