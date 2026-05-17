from __future__ import annotations

import json
import pytest
import fakeredis.aioredis
from fastapi.testclient import TestClient

from stt_router.app import app
from stt_router.router import STTRouter
from stt_router.switcher import EngineSwitcher
from tests.fakes.fake_sarvam import FakeSarvamEngine
from tests.fakes.fake_indicconformer import FakeIndicConformerEngine


def _make_test_router() -> STTRouter:
    import asyncio
    rdb = fakeredis.aioredis.FakeRedis()
    switcher = EngineSwitcher(rdb)
    engines = {
        "sarvam": FakeSarvamEngine(transcript="test transcript", confidence=0.90),
        "indicconformer": FakeIndicConformerEngine(),
    }
    return STTRouter(engines, switcher)


@pytest.fixture
def client(monkeypatch):
    import stt_router.app as app_module
    # Patch _build_router so startup() installs our fake router, not real Redis
    monkeypatch.setattr(app_module, "_build_router", _make_test_router)
    with TestClient(app) as c:
        yield c


def test_healthz(client):
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.json()["status"] == "ok"


def test_engine_health(client):
    resp = client.get("/v1/health/engines")
    assert resp.status_code == 200
    body = resp.json()
    assert "engines" in body
    assert len(body["engines"]) >= 1


def test_stt_batch(client):
    audio = b"\x00\x01" * 320
    resp = client.post(
        "/v1/stt/batch",
        files={"file": ("audio.raw", audio, "application/octet-stream")},
        data={"lang": "hi-en", "session_id": "test-sess", "tenant_id": "t1"},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["text"] == "test transcript"
    assert body["engine_used"] == "sarvam"
    assert body["is_final"] is True
