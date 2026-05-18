from __future__ import annotations

import numpy as np
import pytest

from scoring.scorer import Scorer
from scoring.models import CallInput, TEMP_LABELS
from tests.conftest import make_golden_dataset, make_call_input


def test_golden_held_out_accuracy(onnx_model_path: str) -> None:
    """50 labeled call summaries → temperature accuracy ≥ 0.8 on held-out 10 samples."""
    scorer = Scorer(model_path=onnx_model_path)
    X, y = make_golden_dataset(n_per_class=10)

    # Stratified held-out: last 2 per class (indices 8-9, 18-19, 28-29, 38-39, 48-49)
    test_idx = []
    for cls in range(5):
        test_idx.extend(range(cls * 10 + 8, cls * 10 + 10))
    X_test, y_test = X[test_idx], y[test_idx]

    correct = 0
    for row, label in zip(X_test, y_test):
        inp = CallInput(
            session_id="test",
            lead_id="lead",
            tenant_id="t",
            utterance_count=int(row[0]),
            said_not_interested=bool(row[1]),
            showed_interest=bool(row[2]),
            asked_price=bool(row[3]),
            gave_contact=bool(row[4]),
            should_handover=bool(row[5]),
            should_create_site_visit=bool(row[6]),
            call_duration_secs=float(row[7]),
            avg_confidence=float(row[8]),
            budget_mentioned=bool(row[9]),
        )
        result, _ = scorer.score_call(inp)
        predicted_idx = TEMP_LABELS.index(result.temperature)
        if predicted_idx == int(label):
            correct += 1

    accuracy = correct / len(y_test)
    assert accuracy >= 0.8, f"Golden accuracy {accuracy:.2f} < 0.8"


def test_score_result_has_model_metadata(onnx_model_path: str) -> None:
    scorer = Scorer(model_path=onnx_model_path)
    inp = make_call_input(showed_interest=True, asked_price=True, call_duration_secs=200.0)
    result, _ = scorer.score_call(inp)
    assert result.model_id != ""
    assert result.features_hash != ""
    assert len(result.attributions) == 5


def test_attribution_top5_returned(onnx_model_path: str) -> None:
    scorer = Scorer(model_path=onnx_model_path)
    inp = make_call_input(showed_interest=True, should_create_site_visit=True)
    result, _ = scorer.score_call(inp)
    assert len(result.attributions) == 5
    names = {a.feature for a in result.attributions}
    assert names.issubset(set(["utterance_count", "said_not_interested", "showed_interest",
                                "asked_price", "gave_contact", "should_handover",
                                "should_create_site_visit", "call_duration_secs",
                                "avg_confidence", "budget_mentioned"]))
