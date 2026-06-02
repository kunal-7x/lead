"""Shared fixtures + mocks for the pipecat_runtime tests.

Everything network-y is mocked: llm-router SSE + /generate via a fake httpx
transport, Sarvam/ElevenLabs are never imported (we test the provider-SELECTION
helper, not the live services), Redis via fakeredis, NATS via the in-memory
FakePublisher from voice_agent.actions.

These tests deliberately exercise the custom processors through the ``_pipecat_shim``
FrameProcessor base (real pipecat is not installed on the build box). The shim
records pushed frames so we can assert the exact frame sequence each processor emits.
"""

from __future__ import annotations

import os

import httpx
import pytest

# CSO must not fire real Groq calls in tests.
os.environ.setdefault("CSO_ENABLED", "false")

from voice_agent.models import SessionContext  # noqa: E402
from voice_agent.pipecat_runtime.config import PipecatSettings  # noqa: E402


@pytest.fixture
def settings() -> PipecatSettings:
    return PipecatSettings.from_env()


@pytest.fixture
def ctx() -> SessionContext:
    return SessionContext(
        session_id="sess-001",
        tenant_id="tenant-1",
        campaign_id="camp-1",
        project_id="proj-1",
        lead_id="lead-1",
        voice_profile_id="rahul",   # male speaker
        lang="hi-en",
    )


def sse_body(tokens: list[str]) -> bytes:
    """Build a fake SSE stream_text response body (data: lines)."""
    lines = []
    for t in tokens:
        lines.append(f'data: {{"token": {_json(t)}, "done": false}}')
    lines.append('data: {"token": "", "done": true}')
    return ("\n".join(lines) + "\n").encode()


def _json(s: str) -> str:
    import json
    return json.dumps(s)


def make_mock_llm_client(tokens: list[str], brain: dict) -> httpx.AsyncClient:
    """An httpx.AsyncClient whose transport serves the fake llm-router.

    - POST /v1/llm/generate/stream_text → SSE token stream (tokens, then done).
    - POST /v1/llm/generate            → {"brain": brain}.
    Captures the last request payload on the client object as ``last_payload``
    (for both endpoints) so tests can assert the EXACT body built.
    """
    captured: dict = {}

    async def handler(request: httpx.Request) -> httpx.Response:
        import json as _jsonmod
        body = _jsonmod.loads(request.content.decode() or "{}")
        captured["last_payload"] = body
        if request.url.path.endswith("/v1/llm/generate/stream_text"):
            captured["stream_payload"] = body
            return httpx.Response(
                200,
                content=sse_body(tokens),
                headers={"content-type": "text/event-stream"},
            )
        if request.url.path.endswith("/v1/llm/generate"):
            captured["brain_payload"] = body
            return httpx.Response(200, json={"brain": brain})
        return httpx.Response(404)

    client = httpx.AsyncClient(transport=httpx.MockTransport(handler), timeout=5.0)
    client.captured = captured  # type: ignore[attr-defined]
    return client
