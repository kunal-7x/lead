from __future__ import annotations

import pytest

from scoring.publisher import FakePublisher
from scoring.rules import events_for
from scoring.scorer import FakeScorer
from tests.conftest import make_call_input, make_turn_input


async def test_hot_lead_emits_hot_detected(fake_publisher: FakePublisher) -> None:
    scorer = FakeScorer()
    inp = make_call_input(showed_interest=True, asked_price=True)
    result, evts = scorer.score_call(inp)
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    assert "lead.hot.detected" in fake_publisher.subjects()


async def test_super_hot_emits_hot_detected(fake_publisher: FakePublisher) -> None:
    scorer = FakeScorer()
    inp = make_call_input(should_create_site_visit=True, showed_interest=True)
    result, evts = scorer.score_call(inp)
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    assert "lead.hot.detected" in fake_publisher.subjects()


async def test_hot_detected_fires_once_per_call(fake_publisher: FakePublisher) -> None:
    """Each score_call emits lead.hot.detected at most once."""
    scorer = FakeScorer()
    inp = make_call_input(showed_interest=True, asked_price=True)
    _, evts = scorer.score_call(inp)
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    hot_count = fake_publisher.subjects().count("lead.hot.detected")
    assert hot_count == 1


async def test_cold_lead_no_hot_event(fake_publisher: FakePublisher) -> None:
    scorer = FakeScorer()
    inp = make_call_input(said_not_interested=True, utterance_count=8)
    _, evts = scorer.score_call(inp)
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    assert "lead.hot.detected" not in fake_publisher.subjects()


async def test_broker_suspect_event(fake_publisher: FakePublisher) -> None:
    evts = events_for("warm", "broker")
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    assert "lead.suspect.broker" in fake_publisher.subjects()


async def test_fake_suspect_event(fake_publisher: FakePublisher) -> None:
    evts = events_for("bad", "fake")
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    assert "lead.suspect.fake" in fake_publisher.subjects()


async def test_lead_scored_always_emitted(fake_publisher: FakePublisher) -> None:
    scorer = FakeScorer()
    inp = make_call_input()
    _, evts = scorer.score_call(inp)
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    assert "lead.scored" in fake_publisher.subjects()


async def test_turn_hot_emits_event(fake_publisher: FakePublisher) -> None:
    scorer = FakeScorer()
    inp = make_turn_input(showed_interest=True, asked_price=True)
    _, evts = scorer.score_turn(inp)
    for evt in evts:
        await fake_publisher.publish(evt, b"")
    assert "lead.hot.detected" in fake_publisher.subjects()
