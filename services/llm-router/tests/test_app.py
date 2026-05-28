from __future__ import annotations

import json

import pytest
import fakeredis.aioredis
from fastapi.testclient import TestClient

from llm_router.app import app
from llm_router.router import LLMRouter
from llm_router.switcher import ModelSwitcher
from llm_router.kb_client import FakeKbRetriever
from tests.fakes.fake_backends import FakeBackend


def _make_router() -> LLMRouter:
    rdb = fakeredis.aioredis.FakeRedis()
    switcher = ModelSwitcher(rdb)
    backends = {"groq_llama": FakeBackend()}
    return LLMRouter(backends, switcher, FakeKbRetriever())


@pytest.fixture
def client(monkeypatch):
    import llm_router.app as app_module
    monkeypatch.setattr(app_module, "_build_router", _make_router)
    with TestClient(app) as c:
        yield c


def test_healthz(client):
    resp = client.get("/healthz")
    assert resp.status_code == 200


def test_list_models(client):
    resp = client.get("/v1/llm/models")
    assert resp.status_code == 200
    body = resp.json()
    assert "groq_llama" in body["models"]
    assert body["default"] == "groq_llama"


def test_generate(client):
    resp = client.post("/v1/llm/generate", json={
        "user_turn": "2BHK ka price kya hai?",
        "lang": "hi-en",
        "tenant_id": "t1",
        "session_id": "s1",
        "project_id": "proj-1",
    })
    assert resp.status_code == 200
    body = resp.json()
    assert "brain" in body
    # router uses groq_llama fake backend (the only one registered in test fixture)
    assert body["model_used"] == "groq_llama"


def test_generate_stream_sse_format(client):
    """POST /v1/llm/generate/stream returns text/event-stream with token events ending done:true."""
    resp = client.post(
        "/v1/llm/generate/stream",
        json={
            "user_turn": "2BHK ka price kya hai?",
            "lang": "hi-en",
            "tenant_id": "t1",
            "session_id": "s1",
            "project_id": "proj-1",
        },
        headers={"Accept": "text/event-stream"},
    )
    assert resp.status_code == 200
    assert "text/event-stream" in resp.headers.get("content-type", "")

    # Parse SSE lines
    raw = resp.text
    events = []
    for line in raw.splitlines():
        line = line.strip()
        if line.startswith("data:"):
            payload = line[len("data:"):].strip()
            events.append(json.loads(payload))

    assert len(events) >= 1, "must have at least one SSE event"

    # All events except last must have done=false
    for ev in events[:-1]:
        assert ev.get("done") is False, f"intermediate event has done!=false: {ev}"
        assert "token" in ev

    # Final event must have done=true, summary, next_action
    final = events[-1]
    assert final.get("done") is True, f"last event must have done:true, got {final}"
    assert "summary" in final, "final event must include summary"
    assert "next_action" in final, "final event must include next_action"
    assert final.get("token") == "", "final event token must be empty string"


def test_generate_stream_text_sse_format(client):
    """POST /v1/llm/generate/stream_text returns text/event-stream with plain-text tokens.

    Unlike /stream, the final event does NOT carry summary/next_action (those come
    from the parallel batch call in the worker). Final event has only done=true.
    """
    resp = client.post(
        "/v1/llm/generate/stream_text",
        json={
            "user_turn": "2BHK ka price kya hai?",
            "lang": "hi-en",
            "tenant_id": "t1",
            "session_id": "s1",
            "project_id": "proj-1",
        },
        headers={"Accept": "text/event-stream"},
    )
    assert resp.status_code == 200
    assert "text/event-stream" in resp.headers.get("content-type", "")

    raw = resp.text
    events = []
    for line in raw.splitlines():
        line = line.strip()
        if line.startswith("data:"):
            payload = line[len("data:"):].strip()
            events.append(json.loads(payload))

    assert len(events) >= 1, "must have at least one SSE event"

    # All intermediate events must have done=false with a token
    for ev in events[:-1]:
        assert ev.get("done") is False, f"intermediate event has done!=false: {ev}"
        assert "token" in ev

    # Final event must have done=true and empty token
    final = events[-1]
    assert final.get("done") is True, f"last event must have done:true, got {final}"
    assert final.get("token") == "", "final event token must be empty string"
    # Plain-text endpoint does NOT include summary/next_action (worker gets those from batch)
    # (They may or may not be present in the fallback path — we don't assert their absence)
