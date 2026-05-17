from __future__ import annotations

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
    assert body["model_used"] == "groq_llama"
