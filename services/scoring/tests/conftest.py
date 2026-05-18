from __future__ import annotations

import json
import os
import tempfile

import numpy as np
import pytest

from scoring.models import CallInput, TurnInput, FEATURE_NAMES, TEMP_LABELS
from scoring.publisher import FakePublisher
from scoring.scorer import FakeScorer
from scoring.store import FakeStore

# ---------------------------------------------------------------------------
# Synthetic data helpers
# ---------------------------------------------------------------------------

def _make_sample(label: int, rng: np.random.Generator) -> list[float]:
    """Generate a synthetic feature vector consistent with scoring rules."""
    # label: 0=bad, 1=cold, 2=warm, 3=hot, 4=super_hot
    # bad:  said_not_interested=1 + utterance_count in {1,2} → rules return "bad"
    # cold: said_not_interested=1 + utterance_count >=3 → rules return "cold"
    if label == 0:
        utterance_count = float(rng.integers(1, 3))   # 1 or 2 (<3 → bad)
    elif label == 1:
        utterance_count = float(rng.integers(3, 10))  # 3-9 (>=3 → cold)
    else:
        utterance_count = float(rng.integers(5 + label * 3, 10 + label * 10))

    said_not_interested = 1.0 if label <= 1 else 0.0
    showed_interest = 1.0 if label >= 2 else 0.0
    asked_price = 1.0 if label >= 3 else 0.0
    gave_contact = 1.0 if label >= 3 else 0.0
    should_handover = 1.0 if label == 4 else 0.0
    should_create_site_visit = 1.0 if label == 4 else 0.0
    call_duration = float(rng.integers(10 + label * 30, 30 + label * 90))
    avg_confidence = round(float(rng.uniform(0.2 + label * 0.12, 0.4 + label * 0.12)), 2)
    budget_mentioned = 1.0 if label >= 3 else 0.0
    return [
        utterance_count, said_not_interested, showed_interest, asked_price,
        gave_contact, should_handover, should_create_site_visit,
        call_duration, avg_confidence, budget_mentioned,
    ]


def make_golden_dataset(n_per_class: int = 10, seed: int = 42) -> tuple[np.ndarray, np.ndarray]:
    """Returns (X, y) with n_per_class samples per temperature label, seeded."""
    rng = np.random.default_rng(seed)
    X, y = [], []
    for label in range(5):
        for _ in range(n_per_class):
            X.append(_make_sample(label, rng))
            y.append(label)
    return np.array(X, dtype=np.float32), np.array(y, dtype=np.int64)


# ---------------------------------------------------------------------------
# Session-scoped ONNX model fixture
# ---------------------------------------------------------------------------

@pytest.fixture(scope="session")
def onnx_model_path() -> str:
    from sklearn.ensemble import GradientBoostingClassifier
    from skl2onnx import convert_sklearn
    from skl2onnx.common.data_types import FloatTensorType

    X, y = make_golden_dataset(n_per_class=10)
    # Stratified split: 8 per class for training, 2 per class held-out
    # Each class occupies indices [cls*10 : cls*10+10]; use first 8 for train
    train_idx = []
    for cls in range(5):
        train_idx.extend(range(cls * 10, cls * 10 + 8))
    X_train = X[train_idx]
    y_train = y[train_idx]

    clf = GradientBoostingClassifier(n_estimators=50, max_depth=3, random_state=42)
    clf.fit(X_train, y_train)

    initial_type = [("X", FloatTensorType([None, len(FEATURE_NAMES)]))]
    onnx_model = convert_sklearn(clf, initial_types=initial_type, target_opset=17)

    # Store feature importances in model metadata
    meta = onnx_model.metadata_props.add()
    meta.key = "feature_importances"
    meta.value = json.dumps(clf.feature_importances_.tolist())

    tmp = tempfile.NamedTemporaryFile(suffix=".onnx", delete=False)
    tmp.write(onnx_model.SerializeToString())
    tmp.close()

    os.environ["SCORING_MODEL_PATH"] = tmp.name
    return tmp.name


@pytest.fixture
def fake_scorer() -> FakeScorer:
    return FakeScorer()


@pytest.fixture
def fake_publisher() -> FakePublisher:
    return FakePublisher()


@pytest.fixture
def fake_store() -> FakeStore:
    return FakeStore()


def make_call_input(**kwargs) -> CallInput:
    defaults = dict(
        session_id="sess-001",
        lead_id="lead-001",
        tenant_id="tenant-1",
        utterance_count=10,
        call_duration_secs=120.0,
        avg_confidence=0.75,
    )
    defaults.update(kwargs)
    return CallInput(**defaults)


def make_turn_input(**kwargs) -> TurnInput:
    defaults = dict(
        session_id="sess-001",
        lead_id="lead-001",
        tenant_id="tenant-1",
        turn_index=1,
    )
    defaults.update(kwargs)
    return TurnInput(**defaults)
