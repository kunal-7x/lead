from __future__ import annotations

import pytest

from scoring.models import FeatureVector
from scoring.rules import apply_temperature_rules, events_for, infer_buyer_type
from scoring.scorer import FakeScorer
from tests.conftest import make_call_input, make_turn_input


def test_not_interested_short_call_overrides_to_bad() -> None:
    fv = FeatureVector(said_not_interested=1.0, utterance_count=2.0)
    result = apply_temperature_rules(fv, "hot")
    assert result == "bad"


def test_not_interested_long_call_overrides_to_cold() -> None:
    fv = FeatureVector(said_not_interested=1.0, utterance_count=10.0)
    result = apply_temperature_rules(fv, "hot")
    assert result == "cold"


def test_no_not_interested_passes_model_temp() -> None:
    fv = FeatureVector(said_not_interested=0.0, utterance_count=15.0)
    for temp in ("bad", "cold", "warm", "hot", "super_hot"):
        assert apply_temperature_rules(fv, temp) == temp  # type: ignore[arg-type]


def test_fake_scorer_not_interested_is_bad() -> None:
    scorer = FakeScorer()
    inp = make_call_input(said_not_interested=True, utterance_count=1)
    result, evts = scorer.score_call(inp)
    assert result.temperature == "bad"
    assert result.score == 0


def test_fake_scorer_not_interested_long_is_cold() -> None:
    scorer = FakeScorer()
    inp = make_call_input(said_not_interested=True, utterance_count=8)
    result, _ = scorer.score_call(inp)
    assert result.temperature == "cold"


def test_fake_scorer_site_visit_is_super_hot() -> None:
    scorer = FakeScorer()
    inp = make_call_input(should_create_site_visit=True, showed_interest=True)
    result, _ = scorer.score_call(inp)
    assert result.temperature == "super_hot"


def test_fake_scorer_interested_and_price_is_hot() -> None:
    scorer = FakeScorer()
    inp = make_call_input(showed_interest=True, asked_price=True)
    result, _ = scorer.score_call(inp)
    assert result.temperature == "hot"


def test_fake_scorer_interest_only_is_warm() -> None:
    scorer = FakeScorer()
    inp = make_call_input(showed_interest=True)
    result, _ = scorer.score_call(inp)
    assert result.temperature == "warm"


def test_turn_scorer_not_interested_early() -> None:
    scorer = FakeScorer()
    inp = make_turn_input(said_not_interested=True, turn_index=1)
    temp, _ = scorer.score_turn(inp)
    assert temp == "bad"


def test_turn_scorer_not_interested_late() -> None:
    scorer = FakeScorer()
    inp = make_turn_input(said_not_interested=True, turn_index=5)
    temp, _ = scorer.score_turn(inp)
    assert temp == "cold"


def test_infer_buyer_type_fake_for_short_disengaged() -> None:
    fv = FeatureVector(utterance_count=1.0, showed_interest=0.0)
    assert infer_buyer_type(fv) == "fake"


def test_infer_buyer_type_broker_for_long_no_contact() -> None:
    fv = FeatureVector(utterance_count=25.0, gave_contact=0.0, should_create_site_visit=0.0)
    assert infer_buyer_type(fv) == "broker"


def test_infer_buyer_type_self_use_default() -> None:
    fv = FeatureVector(utterance_count=10.0, showed_interest=1.0)
    assert infer_buyer_type(fv) == "self_use"
