from __future__ import annotations

import os
from typing import Protocol

import httpx
from pydantic import BaseModel, Field


class ClaimViolation(BaseModel):
    claim_id: str = ""
    claim_type: str
    reason: str
    action_taken: str
    score: float = 0.0


class ClaimCheckResult(BaseModel):
    ok: bool
    action_taken: str = "none"
    violations: list[ClaimViolation] = Field(default_factory=list)
    rewritten: str | None = None


class ClaimControl(Protocol):
    async def check(
        self,
        *,
        tenant_id: str,
        project_id: str,
        session_id: str,
        channel: str,
        text: str,
    ) -> ClaimCheckResult: ...


class HttpClaimControl:
    def __init__(self, base_url: str | None = None) -> None:
        self._base_url = (base_url or os.getenv("KNOWLEDGE_SERVICE_URL", "http://knowledge:8110")).rstrip("/")
        self._client = httpx.AsyncClient(
            timeout=4.0,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

    async def check(
        self,
        *,
        tenant_id: str,
        project_id: str,
        session_id: str,
        channel: str,
        text: str,
    ) -> ClaimCheckResult:
        if not project_id or not text.strip():
            return ClaimCheckResult(ok=True)
        resp = await self._client.post(
            f"{self._base_url}/v1/claim-control/check",
            json={
                "tenant_id": tenant_id,
                "project_id": project_id,
                "call_id": session_id,
                "channel": channel,
                "text": text,
            },
        )
        resp.raise_for_status()
        return ClaimCheckResult.model_validate(resp.json())

    async def aclose(self) -> None:
        await self._client.aclose()


class AllowAllClaimControl:
    async def check(
        self,
        *,
        tenant_id: str,
        project_id: str,
        session_id: str,
        channel: str,
        text: str,
    ) -> ClaimCheckResult:
        return ClaimCheckResult(ok=True)
