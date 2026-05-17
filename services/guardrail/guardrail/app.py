from __future__ import annotations

from pydantic import BaseModel
from fastapi import FastAPI
from fastapi.responses import JSONResponse

from guardrail.validators import run_all

app = FastAPI(title="guardrail", version="0.1.0")


class GuardrailRequest(BaseModel):
    brain: dict
    kb_chunks: list[str] = []
    user_turn: str = ""
    tenant_id: str = ""
    session_id: str = ""


class GuardrailResponse(BaseModel):
    brain: dict
    action_taken: str
    reason: str


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok"}


@app.post("/v1/guardrail/check", response_model=GuardrailResponse)
async def check(req: GuardrailRequest) -> GuardrailResponse:
    result = run_all(req.brain, req.kb_chunks, req.user_turn)
    return GuardrailResponse(
        brain=result.brain,
        action_taken=result.action_taken,
        reason=result.reason,
    )
