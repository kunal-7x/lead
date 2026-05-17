from __future__ import annotations

import os

import httpx

from llm_router.backends.base import LLMBackend
from llm_router.models import BrainOutput, LLMRequest

_VLLM_URL = os.getenv("VLLM_URL", "http://vllm:8000")
_TIMEOUT = 30.0

# Model name → vLLM served model name
_MODEL_MAP = {
    "qwen3_32b": "Qwen/Qwen2.5-32B-Instruct",
    "llama3_70b": "meta-llama/Llama-3.3-70B-Instruct-AWQ",
    "mistral_7b": "mistralai/Mistral-7B-Instruct-v0.3",
}


class VLLMBackend(LLMBackend):
    """vLLM OpenAI-compatible API — GPU-based self-hosted models.

    Production: rent GPU on RunPod/Vast.ai, ~8hr/day.
    Set VLLM_URL and VLLM_MODEL env vars.
    """

    def __init__(self, model_key: str = "qwen3_32b", base_url: str = "", timeout: float = _TIMEOUT) -> None:
        self.name = model_key
        self._model = _MODEL_MAP.get(model_key, model_key)
        self._base_url = base_url or _VLLM_URL
        self._timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        messages = self._build_messages(req, kb_context)
        payload = {
            "model": self._model,
            "messages": messages,
            "response_format": {"type": "json_object"},
            "temperature": 0.3,
        }
        resp = await self._client.post(f"{self._base_url}/v1/chat/completions", json=payload)
        resp.raise_for_status()
        body = resp.json()
        content = body["choices"][0]["message"]["content"]
        usage = body.get("usage", {})
        brain = BrainOutput.model_validate_json(content)
        return brain, usage.get("prompt_tokens", 0), usage.get("completion_tokens", 0)

    async def health_check(self) -> bool:
        try:
            resp = await self._client.get(f"{self._base_url}/health", timeout=3.0)
            return resp.status_code == 200
        except Exception:
            return False

    async def aclose(self) -> None:
        await self._client.aclose()
