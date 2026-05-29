"""Tests for POST /v1/llm/extract (CampaignContext extraction endpoint)."""
from __future__ import annotations

import json
from unittest.mock import AsyncMock, patch

import pytest
import fakeredis.aioredis
from fastapi.testclient import TestClient

from llm_router.app import app
from llm_router.router import LLMRouter
from llm_router.switcher import ModelSwitcher
from llm_router.kb_client import FakeKbRetriever
from tests.fakes.fake_backends import FakeBackend

_EXPECTED_CONTEXT = {
    "product_description": "Premium 2BHK apartments in Bangalore",
    "offer": "10% early-bird discount",
    "talking_points": ["RERA certified", "Ready to move in"],
    "objection_handling": [
        {"objection": "Too expensive", "response": "We offer flexible EMI options"}
    ],
    "qualifying_questions": ["What is your budget?", "When are you planning to buy?"],
    "persona": "Friendly sales executive",
    "do_not_say": ["guaranteed returns", "competitor pricing"],
    "goal": "Schedule a site visit",
    "language": "English",
    "business_hours": "Mon-Sat 9am-6pm",
}


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


def test_extract_returns_campaign_context_keys(client, monkeypatch):
    """POST /v1/llm/extract returns all CampaignContext keys with extracted values."""
    import llm_router.app as app_module
    from llm_router.extractor import CampaignExtractor

    mock_extractor = AsyncMock(spec=CampaignExtractor)
    mock_extractor.extract = AsyncMock(return_value=_EXPECTED_CONTEXT)
    monkeypatch.setattr(app_module, "_extractor", mock_extractor)

    resp = client.post("/v1/llm/extract", json={
        "text": "We sell premium 2BHK apartments in Bangalore with 10% early-bird discount."
    })

    assert resp.status_code == 200
    body = resp.json()

    # All required keys must be present
    required_keys = {
        "product_description", "offer", "talking_points",
        "objection_handling", "qualifying_questions",
        "persona", "do_not_say", "goal", "language", "business_hours",
    }
    assert required_keys == set(body.keys()), f"missing or extra keys: {set(body.keys()) ^ required_keys}"

    # Check extracted values
    assert body["product_description"] == "Premium 2BHK apartments in Bangalore"
    assert isinstance(body["talking_points"], list)
    assert len(body["talking_points"]) == 2
    assert body["talking_points"][0] == "RERA certified"

    # objection_handling must be list of dicts with objection+response
    assert isinstance(body["objection_handling"], list)
    assert len(body["objection_handling"]) == 1
    assert "objection" in body["objection_handling"][0]
    assert "response" in body["objection_handling"][0]

    assert isinstance(body["qualifying_questions"], list)
    assert isinstance(body["do_not_say"], list)
    assert body["goal"] == "Schedule a site visit"
    assert body["language"] == "English"


def test_extract_with_schema_hint(client, monkeypatch):
    """POST /v1/llm/extract passes schema_hint to extractor."""
    import llm_router.app as app_module
    from llm_router.extractor import CampaignExtractor

    captured: list[dict] = []

    async def fake_extract(text: str, schema_hint=None):
        captured.append({"text": text, "schema_hint": schema_hint})
        return {
            "product_description": "Test product",
            "offer": "",
            "talking_points": [],
            "objection_handling": [],
            "qualifying_questions": [],
            "persona": "",
            "do_not_say": [],
            "goal": "",
            "language": "",
            "business_hours": "",
        }

    mock_extractor = AsyncMock(spec=CampaignExtractor)
    mock_extractor.extract = fake_extract
    monkeypatch.setattr(app_module, "_extractor", mock_extractor)

    client.post("/v1/llm/extract", json={
        "text": "real estate brief",
        "schema_hint": "focus on property type",
    })

    assert captured[0]["schema_hint"] == "focus on property type"


def test_extract_empty_fields_default_to_empty(client, monkeypatch):
    """Missing fields default to empty string / empty list."""
    import llm_router.app as app_module
    from llm_router.extractor import CampaignExtractor

    empty_context = {
        "product_description": "",
        "offer": "",
        "talking_points": [],
        "objection_handling": [],
        "qualifying_questions": [],
        "persona": "",
        "do_not_say": [],
        "goal": "",
        "language": "",
        "business_hours": "",
    }
    mock_extractor = AsyncMock(spec=CampaignExtractor)
    mock_extractor.extract = AsyncMock(return_value=empty_context)
    monkeypatch.setattr(app_module, "_extractor", mock_extractor)

    resp = client.post("/v1/llm/extract", json={"text": "very sparse brief"})
    assert resp.status_code == 200
    body = resp.json()
    assert body["product_description"] == ""
    assert body["talking_points"] == []
    assert body["objection_handling"] == []
