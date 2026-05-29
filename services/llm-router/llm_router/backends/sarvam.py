from __future__ import annotations

import json
import os
import re
import time

import httpx

from llm_router.backends.base import LLMBackend
from llm_router.models import BrainOutput, LLMRequest

_SARVAM_API_KEY = os.getenv("SARVAM_API_KEY", "")
_URL = "https://api.sarvam.ai/v1/chat/completions"
_MODEL = "sarvam-m"
_TIMEOUT = 20.0

_THINK_BLOCK = re.compile(r"<think\b[^>]*>.*?</think>", re.DOTALL | re.IGNORECASE)


def _strip_reasoning(content: str) -> str:
    """sarvam-m emits a <think>...</think> reasoning preamble before JSON.
    Strip it so BrainOutput JSON parsing succeeds.
    """
    cleaned = _THINK_BLOCK.sub("", content).strip()
    start = cleaned.find("{")
    end = cleaned.rfind("}")
    if start >= 0 and end > start:
        cleaned = cleaned[start : end + 1]
    return cleaned


class SarvamLLMBackend(LLMBackend):
    """Sarvam-M — Indic-optimised LLM, best Hindi/Hinglish reasoning.

    Uses Sarvam's OpenAI-compatible chat completions endpoint.
    Production: set SARVAM_API_KEY env var.
    """
    name = "sarvam_llm"

    def __init__(self, api_key: str = "", timeout: float = _TIMEOUT) -> None:
        self._api_key = api_key or _SARVAM_API_KEY
        self._timeout = timeout
        self._client = httpx.AsyncClient(
            timeout=timeout,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        payload = {
            "model": _MODEL,
            "messages": messages,
            "response_format": {"type": "json_object"},
            "temperature": 0.3,
        }
        headers = {
            "Authorization": f"Bearer {self._api_key}",
            "API-Subscription-Key": self._api_key,
        }
        resp = await self._client.post(_URL, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        content = body["choices"][0]["message"]["content"]
        usage = body.get("usage", {})
        brain = BrainOutput.model_validate_json(_strip_reasoning(content))
        return brain, usage.get("prompt_tokens", 0), usage.get("completion_tokens", 0)

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.sarvam.ai/v1/models",
                headers={"API-Subscription-Key": self._api_key},
                timeout=3.0,
            )
            return resp.status_code < 500
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()
