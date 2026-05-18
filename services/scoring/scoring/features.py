from __future__ import annotations

import numpy as np
from scoring.models import CallInput, FeatureVector


def extract_call_features(inp: CallInput) -> FeatureVector:
    return FeatureVector(
        utterance_count=float(inp.utterance_count),
        said_not_interested=float(inp.said_not_interested),
        showed_interest=float(inp.showed_interest),
        asked_price=float(inp.asked_price),
        gave_contact=float(inp.gave_contact),
        should_handover=float(inp.should_handover),
        should_create_site_visit=float(inp.should_create_site_visit),
        call_duration_secs=float(inp.call_duration_secs),
        avg_confidence=float(inp.avg_confidence),
        budget_mentioned=float(inp.budget_mentioned),
    )


def feature_to_array(fv: FeatureVector) -> np.ndarray:
    return np.array([[
        fv.utterance_count,
        fv.said_not_interested,
        fv.showed_interest,
        fv.asked_price,
        fv.gave_contact,
        fv.should_handover,
        fv.should_create_site_visit,
        fv.call_duration_secs,
        fv.avg_confidence,
        fv.budget_mentioned,
    ]], dtype=np.float32)
