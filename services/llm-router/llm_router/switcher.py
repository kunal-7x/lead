from __future__ import annotations

from typing import Any

VALID_MODELS = {
    "qwen3_32b", "llama3_70b", "mistral_7b",
    "groq_llama", "sarvam_105b", "openrouter",
    "openai_gpt4o", "anthropic_claude", "google_gemini",
}
DEFAULT_MODEL = "openrouter"

_GLOBAL_KEY = "llm:global_model"


def _tenant_key(tenant_id: str) -> str:
    return f"llm:tenant:{tenant_id}:model"


class ModelSwitcher:
    """Reads active model from Redis.

    - llm:global_model — default for all tenants (set by super_admin)
    - llm:tenant:{tenant_id}:model — per-tenant override

    Per-tenant key takes priority over global key.
    Changing the Redis key at runtime immediately changes routing.
    """

    def __init__(self, redis: Any) -> None:
        self._redis = redis

    async def active_model(self, tenant_id: str) -> str:
        # Per-tenant override takes priority
        raw = await self._redis.get(_tenant_key(tenant_id))
        if raw is not None:
            name = raw.decode() if isinstance(raw, bytes) else str(raw)
            if name in VALID_MODELS:
                return name

        # Global model
        raw = await self._redis.get(_GLOBAL_KEY)
        if raw is not None:
            name = raw.decode() if isinstance(raw, bytes) else str(raw)
            if name in VALID_MODELS:
                return name

        return DEFAULT_MODEL

    async def set_global_model(self, model: str) -> None:
        _validate(model)
        await self._redis.set(_GLOBAL_KEY, model)

    async def set_tenant_model(self, tenant_id: str, model: str) -> None:
        _validate(model)
        await self._redis.set(_tenant_key(tenant_id), model)

    async def clear_tenant_override(self, tenant_id: str) -> None:
        await self._redis.delete(_tenant_key(tenant_id))


def _validate(model: str) -> None:
    if model not in VALID_MODELS:
        raise ValueError(f"Invalid model: {model!r}. Valid: {VALID_MODELS}")
