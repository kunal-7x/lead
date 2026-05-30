"""Tests for the real NATS-backed event publisher + turn store.

Uses a fake NATS client (monkeypatched onto nats.connect) so no broker is
needed. Verifies subjects + payloads match what the Go consumers expect and
that a connection failure degrades gracefully (never raises into the call).
"""

from __future__ import annotations

import json

import pytest

import voice_agent.events_nats as events_nats
from voice_agent.events_nats import NATSEventPublisher, NatsTurnStore
from voice_agent.models import CallTurn


class _FakeNATS:
    """Minimal stand-in for nats.aio.client.Client."""

    def __init__(self) -> None:
        self.published: list[tuple[str, bytes, dict]] = []
        self.is_connected = True
        self.drained = False

    async def publish(self, subject, payload, headers=None):
        self.published.append((subject, payload, headers or {}))

    async def drain(self):
        self.drained = True
        self.is_connected = False


@pytest.fixture(autouse=True)
def _reset_shared_conn():
    # Each test starts with a clean class-level connection state.
    NATSEventPublisher._shared_nc = None
    NATSEventPublisher._connect_failed = False
    yield
    NATSEventPublisher._shared_nc = None
    NATSEventPublisher._connect_failed = False


async def test_publisher_publishes_subject_and_payload(monkeypatch):
    fake = _FakeNATS()

    async def fake_connect(*args, **kwargs):
        return fake

    monkeypatch.setattr(events_nats.nats, "connect", fake_connect)

    pub = NATSEventPublisher("nats://test:4222")
    body = json.dumps({"session_id": "s1", "status": "warm"}).encode()
    await pub.publish("lead.status.updated", body)

    assert len(fake.published) == 1
    subject, payload, headers = fake.published[0]
    assert subject == "lead.status.updated"
    assert json.loads(payload)["session_id"] == "s1"
    # Dedup header present for JetStream.
    assert headers.get("Nats-Msg-Id", "").startswith("lead.status.updated:")


async def test_publisher_reuses_single_connection(monkeypatch):
    connects = {"n": 0}
    fake = _FakeNATS()

    async def fake_connect(*args, **kwargs):
        connects["n"] += 1
        return fake

    monkeypatch.setattr(events_nats.nats, "connect", fake_connect)

    pub = NATSEventPublisher()
    await pub.publish("call.completed", b'{"session_id":"s1"}')
    await pub.publish("call.whatsapp.requested", b'{"session_id":"s1"}')

    assert connects["n"] == 1  # connection reused across publishes
    assert len(fake.published) == 2


async def test_publisher_degrades_gracefully_on_connect_failure(monkeypatch):
    async def boom(*args, **kwargs):
        raise OSError("connection refused")

    monkeypatch.setattr(events_nats.nats, "connect", boom)

    pub = NATSEventPublisher()
    # Must NOT raise — a NATS outage cannot break a live call.
    await pub.publish("call.completed", b'{"session_id":"s1"}')
    assert NATSEventPublisher._connect_failed is True


async def test_turn_store_publishes_call_turn_recorded(monkeypatch):
    fake = _FakeNATS()

    async def fake_connect(*args, **kwargs):
        return fake

    monkeypatch.setattr(events_nats.nats, "connect", fake_connect)

    store = NatsTurnStore(NATSEventPublisher())
    turn = CallTurn(
        session_id="s1", turn_index=0, speaker="caller",
        transcript="budget 60 lakh", confidence=0.92,
        reply="Site visit book karein?", lead_status="hot",
        next_action="book_site_visit", tts_engine="sarvam_bulbul",
    )
    await store.record_turn(turn)

    assert len(fake.published) == 1
    subject, payload, _ = fake.published[0]
    assert subject == "call.turn.recorded"
    data = json.loads(payload)
    assert data["session_id"] == "s1"
    assert data["transcript"] == "budget 60 lakh"
    assert data["confidence"] == 0.92


async def test_turn_store_complete_call_is_noop(monkeypatch):
    fake = _FakeNATS()

    async def fake_connect(*args, **kwargs):
        return fake

    monkeypatch.setattr(events_nats.nats, "connect", fake_connect)

    store = NatsTurnStore(NATSEventPublisher())
    # complete_call must not emit a duplicate call.completed (brain owns it).
    await store.complete_call("s1", "Hot lead", "book_site_visit")
    assert fake.published == []
