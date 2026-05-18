from __future__ import annotations

import hashlib
import json
import os
from typing import cast

import numpy as np
import onnxruntime as ort

from scoring.features import extract_call_features, feature_to_array
from scoring.models import (
    Attribution,
    BuyerType,
    CallInput,
    FeatureVector,
    FEATURE_NAMES,
    ScoreResult,
    TEMP_LABELS,
    TEMP_TO_SCORE,
    Temperature,
    TurnInput,
)
from scoring.rules import apply_temperature_rules, events_for, infer_buyer_type

_DEFAULT_MODEL_PATH = os.path.join(
    os.path.dirname(__file__), "..", "models", "scoring_model.onnx"
)
_MODEL_ID = "gbdt-v1"


class Scorer:
    def __init__(self, model_path: str | None = None) -> None:
        path = model_path or os.environ.get("SCORING_MODEL_PATH") or _DEFAULT_MODEL_PATH
        self._session = ort.InferenceSession(path)
        self._feature_importances = self._load_importances()
        self._model_id = _MODEL_ID
        self._features_hash = hashlib.md5(
            json.dumps(FEATURE_NAMES).encode()
        ).hexdigest()[:8]

    def _load_importances(self) -> list[float]:
        meta = self._session.get_modelmeta()
        raw = meta.custom_metadata_map.get("feature_importances", "")
        if raw:
            return json.loads(raw)
        return [1.0 / len(FEATURE_NAMES)] * len(FEATURE_NAMES)

    def score_call(self, inp: CallInput) -> tuple[ScoreResult, list[str]]:
        fv = extract_call_features(inp)
        arr = feature_to_array(fv)

        outputs = self._session.run(None, {"X": arr})
        label_idx = int(outputs[0][0])
        label_idx = max(0, min(label_idx, len(TEMP_LABELS) - 1))

        model_temp = cast(Temperature, TEMP_LABELS[label_idx])
        temperature = apply_temperature_rules(fv, model_temp)
        buyer_type = cast(BuyerType, infer_buyer_type(fv))

        score = TEMP_TO_SCORE[temperature]
        attributions = self._compute_attributions(arr)
        evts = events_for(temperature, buyer_type)

        result = ScoreResult(
            lead_id=inp.lead_id,
            session_id=inp.session_id,
            temperature=temperature,
            buyer_type=buyer_type,
            score=score,
            attributions=attributions,
            model_id=self._model_id,
            features_hash=self._features_hash,
        )
        return result, evts

    def score_turn(self, inp: TurnInput) -> tuple[Temperature, list[str]]:
        if inp.said_not_interested:
            temp = cast(Temperature, "bad" if inp.turn_index < 3 else "cold")
        elif inp.should_create_site_visit or inp.should_handover:
            temp = cast(Temperature, "super_hot")
        elif inp.showed_interest and inp.asked_price:
            temp = cast(Temperature, "hot")
        elif inp.showed_interest:
            temp = cast(Temperature, "warm")
        else:
            temp = cast(Temperature, "cold")

        buyer_type = cast(
            BuyerType,
            "self_use" if not inp.should_create_site_visit else "self_use",
        )
        evts = events_for(temp, buyer_type)
        return temp, evts

    def _compute_attributions(self, arr: np.ndarray) -> list[Attribution]:
        values = arr[0].tolist()
        scores = [imp * abs(v) for imp, v in zip(self._feature_importances, values)]
        ranked = sorted(zip(FEATURE_NAMES, scores), key=lambda x: -x[1])
        return [
            Attribution(feature=name, importance=round(s, 4))
            for name, s in ranked[:5]
        ]


class FakeScorer:
    """Deterministic scorer for tests that don't need the ONNX model."""

    def score_call(self, inp: CallInput) -> tuple[ScoreResult, list[str]]:
        fv = extract_call_features(inp)
        if fv.said_not_interested:
            temp = cast(Temperature, "bad" if fv.utterance_count < 3 else "cold")
        elif fv.should_create_site_visit or fv.should_handover:
            temp = cast(Temperature, "super_hot")
        elif fv.showed_interest and fv.asked_price:
            temp = cast(Temperature, "hot")
        elif fv.showed_interest:
            temp = cast(Temperature, "warm")
        else:
            temp = cast(Temperature, "cold")

        buyer_type = cast(BuyerType, infer_buyer_type(fv))
        score = TEMP_TO_SCORE[temp]
        evts = events_for(temp, buyer_type)
        result = ScoreResult(
            lead_id=inp.lead_id,
            session_id=inp.session_id,
            temperature=temp,
            buyer_type=buyer_type,
            score=score,
            attributions=[],
            model_id="fake-v0",
            features_hash="00000000",
        )
        return result, evts

    def score_turn(self, inp: TurnInput) -> tuple[Temperature, list[str]]:
        if inp.said_not_interested:
            temp = cast(Temperature, "bad" if inp.turn_index < 3 else "cold")
        elif inp.should_create_site_visit or inp.should_handover:
            temp = cast(Temperature, "super_hot")
        elif inp.showed_interest and inp.asked_price:
            temp = cast(Temperature, "hot")
        elif inp.showed_interest:
            temp = cast(Temperature, "warm")
        else:
            temp = cast(Temperature, "cold")
        evts = events_for(temp, "self_use")
        return temp, evts
