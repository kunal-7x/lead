from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from scoring.app import app, get_scorer, get_publisher, get_store
from scoring.scorer import FakeScorer
from scoring.publisher import FakePublisher
from scoring.store import FakeStore


def _client() -> TestClient:
    publisher = FakePublisher()
    store = FakeStore()
    scorer = FakeScorer()

    app.dependency_overrides[get_scorer] = lambda: scorer
    app.dependency_overrides[get_publisher] = lambda: publisher
    app.dependency_overrides[get_store] = lambda: store
    return TestClient(app)


def test_score_turn_warm() -> None:
    c = _client()
    resp = c.post("/v1/score/turn", json={
        "session_id": "s1", "lead_id": "l1", "tenant_id": "t1",
        "turn_index": 2, "showed_interest": True,
    })
    assert resp.status_code == 200
    data = resp.json()
    assert data["temperature"] == "warm"
    assert "lead.scored" in data["events"]


def test_score_turn_not_interested() -> None:
    c = _client()
    resp = c.post("/v1/score/turn", json={
        "session_id": "s1", "lead_id": "l1", "tenant_id": "t1",
        "turn_index": 1, "said_not_interested": True,
    })
    assert resp.status_code == 200
    assert resp.json()["temperature"] == "bad"


def test_score_call_hot() -> None:
    c = _client()
    resp = c.post("/v1/score/call", json={
        "session_id": "s1", "lead_id": "l1", "tenant_id": "t1",
        "utterance_count": 12, "call_duration_secs": 180,
        "avg_confidence": 0.8, "showed_interest": True, "asked_price": True,
    })
    assert resp.status_code == 200
    data = resp.json()
    assert data["temperature"] == "hot"
    assert data["score"] == 75
    assert data["model_id"] == "fake-v0"


def test_score_call_creates_history() -> None:
    publisher = FakePublisher()
    store = FakeStore()
    scorer = FakeScorer()

    app.dependency_overrides[get_scorer] = lambda: scorer
    app.dependency_overrides[get_publisher] = lambda: publisher
    app.dependency_overrides[get_store] = lambda: store

    with TestClient(app) as c:
        c.post("/v1/score/call", json={
            "session_id": "s1", "lead_id": "l2", "tenant_id": "t1",
            "showed_interest": True, "asked_price": True,
        })
        resp = c.get("/v1/score/lead/l2")
        assert resp.status_code == 200
        data = resp.json()
        assert data["lead_id"] == "l2"
        assert data["latest"] is not None
        assert len(data["history"]) == 1


def test_get_lead_not_found() -> None:
    c = _client()
    resp = c.get("/v1/score/lead/nobody")
    assert resp.status_code == 404


def test_score_call_super_hot_events() -> None:
    publisher = FakePublisher()
    store = FakeStore()
    scorer = FakeScorer()

    app.dependency_overrides[get_scorer] = lambda: scorer
    app.dependency_overrides[get_publisher] = lambda: publisher
    app.dependency_overrides[get_store] = lambda: store

    with TestClient(app) as c:
        resp = c.post("/v1/score/call", json={
            "session_id": "s1", "lead_id": "l3", "tenant_id": "t1",
            "should_create_site_visit": True, "showed_interest": True,
        })
        assert resp.status_code == 200
        assert "lead.hot.detected" in publisher.subjects()
