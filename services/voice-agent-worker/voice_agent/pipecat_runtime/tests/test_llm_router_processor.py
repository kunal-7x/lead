"""LlmRouterProcessor: exact payload, TextFrame emission from fake SSE, brackets,
brain frame, slot accumulation, interruption cancel."""

from __future__ import annotations

import asyncio

import pytest

from voice_agent.pipecat_runtime._pipecat_shim import (
    FrameDirection,
    LLMFullResponseEndFrame,
    LLMFullResponseStartFrame,
    StartInterruptionFrame,
    TextFrame,
    TranscriptionFrame,
)
from voice_agent.pipecat_runtime.cso_processor import CallContext
from voice_agent.pipecat_runtime.frames import BrainOutputFrame
from voice_agent.pipecat_runtime.llm_router_service import LlmRouterProcessor

from .conftest import make_mock_llm_client

pytestmark = pytest.mark.asyncio

_BRAIN = {
    "reply": "Aapka budget kya hai?",
    "lead_status": "warm",
    "lead_score": 40,
    "next_action": "qualify",
    "should_send_whatsapp": False,
    "budget": {"value": 5000000, "text": "50 lakh"},
    "location_pref": "Pune",
}


async def _run_turn(proc: LlmRouterProcessor, text: str):
    await proc.process_frame(TranscriptionFrame(text=text), FrameDirection.DOWNSTREAM)
    # The generation is a background task created inside process_frame.
    assert proc._gen_task is not None
    await proc._gen_task


async def test_builds_exact_payload(ctx, settings):
    client = make_mock_llm_client(["Aapka ", "budget ", "kya hai?"], _BRAIN)
    cc = CallContext(session=ctx, settings=settings)
    cc.cso_directive = "TERSE_DIRECTIVE"
    proc = LlmRouterProcessor(cc, http_client=client)

    await _run_turn(proc, "mujhe ghar chahiye")

    payload = client.captured["stream_payload"]  # type: ignore[attr-defined]
    # EXACT key set per the design-doc contract.
    assert payload["user_turn"] == "mujhe ghar chahiye"
    assert payload["lang"] == "hi-en"
    assert payload["tenant_id"] == "tenant-1"
    assert payload["session_id"] == "sess-001"
    assert payload["project_id"] == "proj-1"
    assert payload["system_prompt_version"] == "v1"
    assert payload["persona_gender"] == "male"          # rahul → male
    assert payload["system_prompt_suffix"] == "TERSE_DIRECTIVE"
    assert payload["dialog_history"] == []              # prior context empty on turn 1
    # The parallel /generate POST gets the SAME body shape.
    assert client.captured["brain_payload"]["user_turn"] == "mujhe ghar chahiye"  # type: ignore[attr-defined]


async def test_emits_textframes_bracketed(ctx, settings):
    client = make_mock_llm_client(["Aapka ", "budget ", "kya hai?"], _BRAIN)
    cc = CallContext(session=ctx, settings=settings)
    proc = LlmRouterProcessor(cc, http_client=client)

    await _run_turn(proc, "hello")

    kinds = [type(f) for f, _ in proc.pushed_frames]
    assert kinds[0] is LLMFullResponseStartFrame
    assert LLMFullResponseEndFrame in kinds
    texts = [f.text for f, _ in proc.pushed_frames if isinstance(f, TextFrame)]
    assert texts == ["Aapka", "budget", "kya hai?"]  # ProsodyShaper strips, keeps words
    # Brain frame emitted for ActionsProcessor.
    brains = [f.brain for f, _ in proc.pushed_frames if isinstance(f, BrainOutputFrame)]
    assert len(brains) == 1
    assert brains[0].reply == "Aapka budget kya hai?"


async def test_dialog_history_accumulates_and_windows(ctx, settings):
    client = make_mock_llm_client(["ok"], _BRAIN)
    settings.llm_history_max_msgs = 4
    cc = CallContext(session=ctx, settings=settings)
    proc = LlmRouterProcessor(cc, http_client=client)

    for i in range(5):
        await _run_turn(proc, f"turn {i}")

    # Window cap respected (<= max).
    assert len(cc.dialog_history) <= 4
    # Last entries are the most recent user+assistant.
    assert cc.dialog_history[-1]["role"] == "assistant"


async def test_slots_accumulate(ctx, settings):
    client = make_mock_llm_client(["ok"], _BRAIN)
    cc = CallContext(session=ctx, settings=settings)
    proc = LlmRouterProcessor(cc, http_client=client)

    await _run_turn(proc, "50 lakh budget, Pune mein")

    assert cc.collected_slots["budget"] == {"value": 5000000, "text": "50 lakh"}
    assert cc.collected_slots["location_pref"] == "Pune"


async def test_interruption_cancels_inflight(ctx, settings):
    # A transport that blocks forever on the SSE stream so we can interrupt it.
    import httpx

    started = asyncio.Event()

    async def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path.endswith("/v1/llm/generate/stream_text"):
            started.set()

            async def gen():
                yield b'data: {"token": "hi", "done": false}\n'
                await asyncio.sleep(10)  # hang — simulate slow stream
                yield b'data: {"token": "", "done": true}\n'

            return httpx.Response(200, content=gen(), headers={"content-type": "text/event-stream"})
        return httpx.Response(200, json={"brain": _BRAIN})

    client = httpx.AsyncClient(transport=httpx.MockTransport(handler), timeout=15.0)
    cc = CallContext(session=ctx, settings=settings)
    proc = LlmRouterProcessor(cc, http_client=client)

    await proc.process_frame(TranscriptionFrame(text="hi"), FrameDirection.DOWNSTREAM)
    await asyncio.wait_for(started.wait(), timeout=2.0)
    gen_task = proc._gen_task
    assert gen_task is not None and not gen_task.done()

    # Barge-in: StartInterruptionFrame must cancel the in-flight generation.
    await proc.process_frame(StartInterruptionFrame(), FrameDirection.DOWNSTREAM)
    assert proc._gen_task is None
    assert gen_task.cancelled() or gen_task.done()
    await client.aclose()
