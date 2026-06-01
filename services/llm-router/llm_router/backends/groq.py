from __future__ import annotations

import itertools
import json
import os
import time

import httpx

from llm_router.backends.base import LLMBackend
from llm_router.models import BrainOutput, LLMRequest

_GROQ_API_KEY = os.getenv("GROQ_API_KEY", "")
_GROQ_API_KEY_2 = os.getenv("GROQ_API_KEY_2", "")
_GROQ_API_KEY_3 = os.getenv("GROQ_API_KEY_3", "")
_GROQ_URL = "https://api.groq.com/openai/v1/chat/completions"
_SCOUT_MODEL = "meta-llama/llama-4-scout-17b-16e-instruct"
_INSTANT_MODEL = "llama-3.1-8b-instant"
_TIMEOUT = 20.0


def _build_key_cycle(*keys: str) -> list[str]:
    """Return a list of non-empty keys in supplied order."""
    result = [k for k in keys if k]
    return result or [""]


class GroqLlamaBackend(LLMBackend):
    """Groq Llama 4 Scout — primary default backend, ~594-750 tok/s, 131k ctx.

    Uses three Groq keys in round-robin (GROQ_API_KEY + GROQ_API_KEY_2 + GROQ_API_KEY_3).
    On 429 from one key the next key is tried before falling to the next chain member,
    effectively tripling the free-tier rate-limit headroom.

    Production: set GROQ_API_KEY, GROQ_API_KEY_2, GROQ_API_KEY_3 env vars.
    """
    name = "groq_llama"

    def __init__(self, api_key: str = "", api_key_2: str = "", api_key_3: str = "", timeout: float = _TIMEOUT) -> None:
        k1 = api_key or _GROQ_API_KEY
        k2 = api_key_2 or _GROQ_API_KEY_2
        k3 = api_key_3 or _GROQ_API_KEY_3
        self._keys = _build_key_cycle(k1, k2, k3)
        # Stateful round-robin counter (index into self._keys)
        self._key_index = 0
        self._api_key = self._keys[0]  # kept for health_check / legacy attr reads
        self._timeout = timeout
        self._client = httpx.AsyncClient(
            timeout=timeout,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )
        self._model = _SCOUT_MODEL
        self._url = _GROQ_URL

    def _next_key(self) -> str:
        """Return the next key in round-robin order."""
        key = self._keys[self._key_index % len(self._keys)]
        self._key_index += 1
        return key

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        payload = {
            "model": self._model,
            "messages": messages,
            "response_format": {"type": "json_object"},
            "temperature": 0.3,
        }
        last_exc: Exception | None = None
        for key in self._keys:
            headers = {"Authorization": f"Bearer {key}"}
            try:
                resp = await self._client.post(_GROQ_URL, json=payload, headers=headers)
                if resp.status_code == 429:
                    last_exc = RuntimeError(f"Groq 429 on key index {self._keys.index(key)}")
                    continue
                resp.raise_for_status()
                body = resp.json()
                content = body["choices"][0]["message"]["content"]
                usage = body.get("usage", {})
                brain = BrainOutput.model_validate_json(content)
                return brain, usage.get("prompt_tokens", 0), usage.get("completion_tokens", 0)
            except (RuntimeError,) as exc:
                last_exc = exc
                continue
        raise last_exc or RuntimeError("All Groq keys failed")

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.groq.com/openai/v1/models",
                headers={"Authorization": f"Bearer {self._api_key}"},
                timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()


class GroqInstantBackend(GroqLlamaBackend):
    """Groq Llama 3.1 8B Instant — kept for compatibility but NOT in active chain.

    Selectable via LLM_ACTIVE_MODEL=groq_instant if needed for testing.
    Removed from the default fallback chain (low token limits, rate-limits fast).
    """
    name = "groq_instant"

    def __init__(self, api_key: str = "", api_key_2: str = "", api_key_3: str = "", timeout: float = _TIMEOUT) -> None:
        super().__init__(api_key=api_key, api_key_2=api_key_2, api_key_3=api_key_3, timeout=timeout)
        self._model = _INSTANT_MODEL
