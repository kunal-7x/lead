from __future__ import annotations

import os

import httpx

from llm_router.backends.base import LLMBackend
from llm_router.models import BrainOutput, LLMRequest

_OPENAI_KEY = os.getenv("OPENAI_API_KEY", "")
_URL = "https://api.openai.com/v1/chat/completions"
_MODEL = "gpt-4o"


class OpenAIBackend(LLMBackend):
    name = "openai_gpt4o"

    def __init__(self, api_key: str = "") -> None:
        self._api_key = api_key or _OPENAI_KEY
        self._client = httpx.AsyncClient(timeout=30.0)

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        payload = {"model": _MODEL, "messages": messages,
                   "response_format": {"type": "json_object"}, "temperature": 0.3}
        headers = {"Authorization": f"Bearer {self._api_key}"}
        resp = await self._client.post(_URL, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        content = body["choices"][0]["message"]["content"]
        usage = body.get("usage", {})
        return BrainOutput.model_validate_json(content), usage.get("prompt_tokens", 0), usage.get("completion_tokens", 0)

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.openai.com/v1/models",
                headers={"Authorization": f"Bearer {self._api_key}"}, timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()


class AnthropicBackend(LLMBackend):
    name = "anthropic_claude"

    def __init__(self, api_key: str = "") -> None:
        self._api_key = api_key or os.getenv("ANTHROPIC_API_KEY", "")
        self._client = httpx.AsyncClient(timeout=30.0)

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        system = messages[0]["content"]
        chat = messages[1:]
        payload = {"model": "claude-sonnet-4-6", "max_tokens": 1024,
                   "system": system, "messages": chat}
        headers = {"x-api-key": self._api_key, "anthropic-version": "2023-06-01"}
        resp = await self._client.post("https://api.anthropic.com/v1/messages",
                                       json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        content = body["content"][0]["text"]
        usage = body.get("usage", {})
        return BrainOutput.model_validate_json(content), usage.get("input_tokens", 0), usage.get("output_tokens", 0)

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                "https://api.anthropic.com/v1/models",
                headers={"x-api-key": self._api_key, "anthropic-version": "2023-06-01"},
                timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()


class OpenRouterBackend(LLMBackend):
    """OpenRouter backend — default LLM provider.

    Env vars:
      LLM_API_KEY / OPENROUTER_API_KEY  — API key (required)
      LLM_MODEL                          — model slug (default google/gemini-2.0-flash-001)
      LLM_BASE_URL                       — completions endpoint (default OpenRouter)
    """
    name = "openrouter"

    _DEFAULT_BASE_URL = "https://openrouter.ai/api/v1/chat/completions"
    _DEFAULT_MODEL = "google/gemini-2.0-flash-001"

    def __init__(self, api_key: str = "", model: str = "") -> None:
        self._api_key = (
            api_key
            or os.getenv("LLM_API_KEY")
            or os.getenv("OPENROUTER_API_KEY", "")
        )
        self._model = model or os.getenv("LLM_MODEL", self._DEFAULT_MODEL)
        self._base_url = os.getenv("LLM_BASE_URL", self._DEFAULT_BASE_URL)
        self._client = httpx.AsyncClient(timeout=30.0)

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        payload = {"model": self._model, "messages": messages,
                   "response_format": {"type": "json_object"}, "temperature": 0.3}
        headers = {"Authorization": f"Bearer {self._api_key}"}
        resp = await self._client.post(self._base_url, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        content = body["choices"][0]["message"]["content"]
        usage = body.get("usage", {})
        return BrainOutput.model_validate_json(content), usage.get("prompt_tokens", 0), usage.get("completion_tokens", 0)

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            # Derive models list URL from base_url (strip path, use /models)
            models_url = self._base_url.rsplit("/chat/completions", 1)[0] + "/models"
            resp = await self._client.get(
                models_url,
                headers={"Authorization": f"Bearer {self._api_key}"}, timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()


class GoogleGeminiBackend(LLMBackend):
    name = "google_gemini"

    def __init__(self, api_key: str = "") -> None:
        self._api_key = api_key or os.getenv("GOOGLE_API_KEY", "")
        self._client = httpx.AsyncClient(timeout=30.0)

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        # Convert to Gemini format
        parts = [{"text": m["content"]} for m in messages]
        payload = {
            "contents": [{"role": "user", "parts": parts}],
            "generationConfig": {"responseMimeType": "application/json"},
        }
        url = f"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent?key={self._api_key}"
        resp = await self._client.post(url, json=payload)
        resp.raise_for_status()
        body = resp.json()
        content = body["candidates"][0]["content"]["parts"][0]["text"]
        return BrainOutput.model_validate_json(content), 0, 0

    async def health_check(self) -> bool:
        if not self._api_key:
            return False
        try:
            resp = await self._client.get(
                f"https://generativelanguage.googleapis.com/v1beta/models?key={self._api_key}",
                timeout=3.0,
            )
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()
