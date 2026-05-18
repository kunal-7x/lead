from __future__ import annotations

from scoring.models import BuyerType, FeatureVector, Temperature


def apply_temperature_rules(fv: FeatureVector, model_temp: Temperature) -> Temperature:
    """Explicit signal overrides model output."""
    if fv.said_not_interested:
        return "bad" if fv.utterance_count < 3 else "cold"
    return model_temp


def infer_buyer_type(fv: FeatureVector) -> BuyerType:
    """Heuristic buyer-type classification from call features."""
    # Very short call with no engagement → likely fake
    if fv.utterance_count < 2 and not fv.showed_interest:
        return "fake"
    # High utterance count, no contact given, no site visit → likely broker probing
    if fv.utterance_count > 20 and not fv.gave_contact and not fv.should_create_site_visit:
        return "broker"
    # Strong purchase signals with budget → self_use or investment
    if fv.budget_mentioned and fv.showed_interest:
        return "investment" if fv.utterance_count > 12 else "self_use"
    return "self_use"


def events_for(temperature: Temperature, buyer_type: BuyerType) -> list[str]:
    evts: list[str] = ["lead.scored"]
    if temperature in ("hot", "super_hot"):
        evts.append("lead.hot.detected")
    if buyer_type == "broker":
        evts.append("lead.suspect.broker")
    if buyer_type == "fake":
        evts.append("lead.suspect.fake")
    return evts
