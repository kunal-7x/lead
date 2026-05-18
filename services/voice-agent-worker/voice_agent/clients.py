from __future__ import annotations

import os
from typing import Protocol

import httpx

from voice_agent.models import STTResult, BrainOutput, TTSResult, SessionContext

_STT_URL = os.getenv("STT_ROUTER_URL", "http://stt-router:8110")
_LLM_URL = os.getenv("LLM_ROUTER_URL", "http://llm-router:8111")
_GUARDRAIL_URL = os.getenv("GUARDRAIL_URL", "http://guardrail:8112")
_TTS_URL = os.getenv("TTS_ROUTER_URL", "http://tts-router:8113")


class STTClient(Protocol):
    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult: ...


class LLMClient(Protocol):
    async def generate(self, ctx: SessionContext, user_turn: str,
                       dialog_history: list[dict]) -> BrainOutput: ...


class GuardrailClient(Protocol):
    async def check(self, brain: BrainOutput, kb_chunks: list[str],
                    user_turn: str) -> BrainOutput: ...


class TTSClient(Protocol):
    async def synthesize(self, text: str, lang: str, voice_id: str,
                         tenant_id: str, session_id: str,
                         tts_premium: bool) -> TTSResult: ...


# ── HTTP implementations ──────────────────────────────────────────────────────

class HttpSTTClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _STT_URL
        self._client = httpx.AsyncClient(timeout=10.0)

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        resp = await self._client.post(
            f"{self._base_url}/v1/stt/batch",
            files={"file": ("audio.raw", audio, "application/octet-stream")},
            data={"lang": lang, "session_id": session_id, "tenant_id": ""},
        )
        resp.raise_for_status()
        return STTResult(**resp.json())

    async def aclose(self) -> None:
        await self._client.aclose()


class HttpLLMClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _LLM_URL
        self._client = httpx.AsyncClient(timeout=20.0)

    async def generate(self, ctx: SessionContext, user_turn: str,
                       dialog_history: list[dict]) -> BrainOutput:
        payload = {
            "user_turn": user_turn,
            "lang": ctx.lang,
            "tenant_id": ctx.tenant_id,
            "session_id": ctx.session_id,
            "project_id": ctx.project_id,
            "system_prompt_version": ctx.system_prompt_version,
            "dialog_history": dialog_history,
        }
        resp = await self._client.post(f"{self._base_url}/v1/llm/generate", json=payload)
        resp.raise_for_status()
        body = resp.json()
        return BrainOutput(**body["brain"])

    async def aclose(self) -> None:
        await self._client.aclose()


class HttpGuardrailClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _GUARDRAIL_URL
        self._client = httpx.AsyncClient(timeout=5.0)

    async def check(self, brain: BrainOutput, kb_chunks: list[str],
                    user_turn: str) -> BrainOutput:
        payload = {
            "brain": brain.model_dump(),
            "kb_chunks": kb_chunks,
            "user_turn": user_turn,
        }
        resp = await self._client.post(f"{self._base_url}/v1/guardrail/check", json=payload)
        resp.raise_for_status()
        body = resp.json()
        return BrainOutput(**body["brain"])

    async def aclose(self) -> None:
        await self._client.aclose()


class HttpTTSClient:
    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _TTS_URL
        self._client = httpx.AsyncClient(timeout=10.0)

    async def synthesize(self, text: str, lang: str, voice_id: str,
                         tenant_id: str, session_id: str,
                         tts_premium: bool = False) -> TTSResult:
        payload = {
            "text": text, "lang": lang, "voice_id": voice_id,
            "tenant_id": tenant_id, "session_id": session_id,
            "tts_premium": tts_premium,
        }
        resp = await self._client.post(f"{self._base_url}/v1/tts/synthesize", json=payload)
        resp.raise_for_status()
        return TTSResult(
            audio=resp.content,
            tier_used=resp.headers.get("X-Tier-Used", ""),
            cache_hit=resp.headers.get("X-Cache-Hit", "false") == "true",
        )

    async def aclose(self) -> None:
        await self._client.aclose()
