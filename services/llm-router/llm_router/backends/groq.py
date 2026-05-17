from __future__ import annotations

import json
import os
import time

import httpx

from llm_router.backends.base import LLMBackend
from llm_router.models import BrainOutput, LLMRequest

_GROQ_API_KEY = os.getenv("GROQ_API_KEY", "")
_GROQ_URL = "https://api.groq.com/openai/v1/chat/completions"
_MODEL = "llama-3.3-70b-versatile"
_TIMEOUT = 20.0


class GroqLlamaBackend(LLMBackend):
    """Groq Llama 3.3 — default backend, ~275 tokens/s, no GPU needed.

    Production: set GROQ_API_KEY env var.
    This is the default when no GPU infrastructure is available.
    """
    name = "groq_llama"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT) -> None:
        self._api_key = api_key or _GROQ_API_KEY
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        payload = {
            "model": _MODEL,
            "messages": messages,
            "response_format": {"type": "json_object"},
            "temperature": 0.3,
        }
        headers = {"Authorization": f"Bearer {self._api_key}"}
        resp = await self._client.post(_GROQ_URL, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        content = body["choices"][0]["message"]["content"]
        usage = body.get("usage", {})
        brain = BrainOutput.model_validate_json(content)
        return brain, usage.get("prompt_tokens", 0), usage.get("completion_tokens", 0)

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
