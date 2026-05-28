from __future__ import annotations

import json
import os
import time

import httpx

from llm_router.backends.base import LLMBackend
from llm_router.models import BrainOutput, LLMRequest

_CEREBRAS_API_KEY = os.getenv("CEREBRAS_API_KEY", "")
_CEREBRAS_URL = "https://api.cerebras.ai/v1/chat/completions"
_MODEL = "llama-3.3-70b"
_TIMEOUT = 20.0


class CerebrasBackend(LLMBackend):
    """Cerebras Llama 3.3 — fallback backend, OpenAI-compatible endpoint.

    Production: set CEREBRAS_API_KEY env var.
    Used as fallback when Groq is unavailable or rate-limited.
    """
    name = "cerebras_llama"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT) -> None:
        self._api_key = api_key or _CEREBRAS_API_KEY
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)
        self._model = _MODEL
        self._url = _CEREBRAS_URL

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        payload = {
            "model": _MODEL,
            "messages": messages,
            "response_format": {"type": "json_object"},
            "temperature": 0.3,
        }
        headers = {"Authorization": f"Bearer {self._api_key}"}
        resp = await self._client.post(_CEREBRAS_URL, json=payload, headers=headers)
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
                "https://api.cerebras.ai/v1/models",
                headers={"Authorization": f"Bearer {self._api_key}"},
                timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()
