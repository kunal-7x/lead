from __future__ import annotations

import os
from typing import Protocol

import httpx

from llm_router.models import KbChunk

_KB_URL = os.getenv("KNOWLEDGE_SERVICE_URL", "http://knowledge:8104")
_TOP_K = 5


class KbRetriever(Protocol):
    async def retrieve(self, project_id: str, query: str, lang: str, top_k: int) -> list[KbChunk]: ...


class HttpKbRetriever:
    """Calls the Go knowledge service RetrieveKb endpoint.

    Production: set KNOWLEDGE_SERVICE_URL env var.
    Tests: inject FakeKbRetriever.
    """

    def __init__(self, base_url: str = "") -> None:
        self._base_url = base_url or _KB_URL
        self._client = httpx.AsyncClient(
            timeout=5.0,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

    async def retrieve(self, project_id: str, query: str, lang: str, top_k: int = _TOP_K) -> list[KbChunk]:
        if not project_id:
            return []
        payload = {"query": query, "lang": lang, "top_k": top_k}
        resp = await self._client.post(
            f"{self._base_url}/v1/knowledge/projects/{project_id}/retrieve",
            json=payload,
        )
        resp.raise_for_status()
        data = resp.json()
        return [KbChunk(text=c["text"], source=c.get("source", ""), score=c.get("score", 1.0))
                for c in data.get("chunks", [])]

    async def aclose(self) -> None:
        await self._client.aclose()


class FakeKbRetriever:
    """In-memory KB retriever for tests."""

    def __init__(self, chunks: list[KbChunk] | None = None) -> None:
        self.chunks = chunks or [
            KbChunk(text="2BHK flat available at 60 lakh in Pune", source="inventory"),
            KbChunk(text="RERA registered project MH/12345", source="facts"),
        ]
        self.call_count = 0

    async def retrieve(self, project_id: str, query: str, lang: str, top_k: int = _TOP_K) -> list[KbChunk]:
        self.call_count += 1
        return self.chunks[:top_k]
