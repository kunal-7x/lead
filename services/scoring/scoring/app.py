from __future__ import annotations

import json

from fastapi import Depends, FastAPI, HTTPException

from scoring.models import CallInput, LeadHistory, ScoreResult, TurnInput
from scoring.publisher import FakePublisher, Publisher
from scoring.scorer import FakeScorer, Scorer
from scoring.store import FakeStore

app = FastAPI(title="scoring", version="0.1.0")

_scorer: Scorer | FakeScorer = FakeScorer()
_publisher: Publisher = FakePublisher()
_store: FakeStore = FakeStore()


def get_scorer() -> Scorer | FakeScorer:
    return _scorer


def get_publisher() -> Publisher:
    return _publisher


def get_store() -> FakeStore:
    return _store


@app.post("/v1/score/turn")
async def score_turn(
    body: TurnInput,
    scorer: Scorer | FakeScorer = Depends(get_scorer),
    publisher: Publisher = Depends(get_publisher),
) -> dict:
    temperature, events = scorer.score_turn(body)
    for evt in events:
        await publisher.publish(evt, json.dumps({"lead_id": body.lead_id}).encode())
    return {"lead_id": body.lead_id, "temperature": temperature, "events": events}


@app.post("/v1/score/call", response_model=ScoreResult)
async def score_call(
    body: CallInput,
    scorer: Scorer | FakeScorer = Depends(get_scorer),
    publisher: Publisher = Depends(get_publisher),
    store: FakeStore = Depends(get_store),
) -> ScoreResult:
    result, events = scorer.score_call(body)
    await store.save(result)
    for evt in events:
        await publisher.publish(
            evt, json.dumps({"lead_id": body.lead_id, "session_id": body.session_id}).encode()
        )
    return result


@app.get("/v1/score/lead/{lead_id}", response_model=LeadHistory)
async def get_lead_history(
    lead_id: str,
    store: FakeStore = Depends(get_store),
) -> LeadHistory:
    history = await store.get_history(lead_id)
    if history.latest is None:
        raise HTTPException(status_code=404, detail="lead not found")
    return history
