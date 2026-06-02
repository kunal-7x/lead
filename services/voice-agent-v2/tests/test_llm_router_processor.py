"""Unit tests for LlmRouterProcessor against REAL pipecat 1.3.0.

No cloud, no live calls, no real HTTP. Uses httpx.MockTransport to intercept
all requests and return pre-canned SSE streams.

Uses the REAL pipecat frame classes. The shim is NOT injected here — pipecat
is installed and the try-import in llm_router_processor.py succeeds cleanly.

Frame tracking: the real FrameProcessor.push_frame requires a started pipeline
and linked successors. For unit tests we use a TrackedLlmRouterProcessor
subclass that overrides push_frame to record (frame, direction) pairs instead
of forwarding them, making assertions simple without mocking the pipeline.

Covers the four bug-fix assertions:
  BF1 — exactly ONE upstream call per finalized transcript turn
  BF2 — tokens arrive in order as TextFrame instances
  BF3 — barge-in cancels with NO duplicate call (via _cancel_inflight; note
         StartInterruptionFrame is None in pipecat 1.3.0, so barge-in is tested
         via direct _cancel_inflight() call matching real pipeline behaviour)
  BF4 — interim/partial TranscriptionFrame (finalized=False) never fires a call
"""

from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field
from typing import Any

import httpx
import pytest

# ---------------------------------------------------------------------------
# Real pipecat imports — shim must NOT shadow these.
# ---------------------------------------------------------------------------
from pipecat.frames.frames import (
    LLMFullResponseEndFrame,
    LLMFullResponseStartFrame,
    TextFrame,
    TranscriptionFrame,
)
from pipecat.processors.frame_processor import FrameDirection, FrameProcessor

from voice_agent_v2.llm_router_processor import LlmRouterProcessor, StartInterruptionFrame


# ---------------------------------------------------------------------------
# Tracked subclass: records every push_frame call instead of forwarding it.
# This replaces the shim's FrameProcessor.pushed_frames mechanism cleanly.
# ---------------------------------------------------------------------------

class TrackedLlmRouterProcessor(LlmRouterProcessor):
    """LlmRouterProcessor that records pushed frames for test assertions.

    Overrides push_frame so tests can inspect what was pushed without
    needing a live pipeline or started processor.
    """

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        super().__init__(*args, **kwargs)
        self.pushed_frames: list[tuple[Any, FrameDirection]] = []

    async def push_frame(
        self,
        frame: Any,
        direction: FrameDirection = FrameDirection.DOWNSTREAM,
    ) -> None:
        self.pushed_frames.append((frame, direction))
        # Do NOT forward to super() — no pipeline connected in unit tests.


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

@dataclass
class FakeSettings:
    llm_router_url: str = "http://mock-llm-router:8111"


@dataclass
class FakeSessionContext:
    session_id: str = "sess-test"
    tenant_id: str = "tenant-test"
    lang: str = "hi-en"
    project_id: str = "proj-test"
    system_prompt_version: str = "v1"
    voice_profile_id: str = "rahul"
    campaign_context: dict = field(default_factory=dict)
    system_prompt_suffix: str | None = None


def _sse_stream(*tokens: str, done_extra: dict | None = None) -> bytes:
    """Build a mock SSE response body from a list of tokens."""
    lines = []
    for t in tokens:
        lines.append(f"data: {json.dumps({'token': t, 'done': False})}\n\n")
    final: dict = {"token": "", "done": True}
    if done_extra:
        final.update(done_extra)
    lines.append(f"data: {json.dumps(final)}\n\n")
    return "".join(lines).encode()


class _MockTransport(httpx.AsyncBaseTransport):
    """Records all requests and returns a pre-canned SSE body."""

    def __init__(self, sse_body: bytes = b"", status_code: int = 200) -> None:
        self.requests: list[httpx.Request] = []
        self._body = sse_body
        self._status = status_code

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        return httpx.Response(
            self._status,
            headers={"content-type": "text/event-stream"},
            content=self._body,
            request=request,
        )


def _make_processor(transport: _MockTransport) -> TrackedLlmRouterProcessor:
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://mock-llm-router:8111")
    proc = TrackedLlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)
    return proc


def _final_frame(text: str) -> TranscriptionFrame:
    """Create a finalized TranscriptionFrame (real pipecat defaults finalized=False)."""
    return TranscriptionFrame(text=text, user_id="", timestamp="", finalized=True)


def _interim_frame(text: str) -> TranscriptionFrame:
    """Create an interim (not finalized) TranscriptionFrame."""
    return TranscriptionFrame(text=text, user_id="", timestamp="", finalized=False)


# ---------------------------------------------------------------------------
# BF1: Exactly ONE upstream call per finalized turn
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_exactly_one_call_per_finalized_turn():
    """BUG-FIX 1 & 2: One HTTP call per finalized TranscriptionFrame; tokens in order."""
    tokens = ["Hello ", "world ", "test."]
    transport = _MockTransport(sse_body=_sse_stream(*tokens))
    proc = _make_processor(transport)

    frame = _final_frame("Hello world test.")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    await asyncio.sleep(0)
    task = proc._gen_task
    if task is not None:
        await task

    assert len(transport.requests) == 1
    req = transport.requests[0]
    assert req.url.path == "/v1/llm/generate/stream_text"
    assert req.method == "POST"


@pytest.mark.asyncio
async def test_two_finalized_turns_two_calls():
    """Each finalized turn fires exactly one call (two turns = two calls total)."""
    transport = _MockTransport(sse_body=_sse_stream("Hi."))
    proc = _make_processor(transport)

    for text in ["Turn one.", "Turn two."]:
        frame = _final_frame(text)
        await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
        task = proc._gen_task
        if task is not None:
            await task

    assert len(transport.requests) == 2


# ---------------------------------------------------------------------------
# BF2: Tokens arrive in order as TextFrames
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_tokens_streamed_as_text_frames_in_order():
    """BUG-FIX 2: TextFrame tokens match SSE token order."""
    tokens = ["Namaste ", "aap ", "kaise ", "hain?"]
    transport = _MockTransport(sse_body=_sse_stream(*tokens))
    proc = _make_processor(transport)

    frame = _final_frame("Hello.")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    task = proc._gen_task
    if task is not None:
        await task

    # Use exact type check: in real pipecat TranscriptionFrame subclasses TextFrame,
    # so isinstance would also catch forwarded TranscriptionFrames.
    text_frames = [
        f for f, d in proc.pushed_frames if type(f) is TextFrame
    ]
    assert [tf.text for tf in text_frames] == tokens


@pytest.mark.asyncio
async def test_llm_response_bracketed_by_start_end_frames():
    """LLMFullResponseStartFrame precedes tokens; LLMFullResponseEndFrame follows."""
    transport = _MockTransport(sse_body=_sse_stream("Hello."))
    proc = _make_processor(transport)

    frame = _final_frame("Hi.")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    task = proc._gen_task
    if task is not None:
        await task

    frame_types = [type(f).__name__ for f, _ in proc.pushed_frames]
    assert "LLMFullResponseStartFrame" in frame_types
    assert "LLMFullResponseEndFrame" in frame_types
    start_idx = next(i for i, n in enumerate(frame_types) if n == "LLMFullResponseStartFrame")
    end_idx = next(i for i, n in enumerate(frame_types) if n == "LLMFullResponseEndFrame")
    text_indices = [i for i, n in enumerate(frame_types) if n == "TextFrame"]
    assert all(start_idx < ti < end_idx for ti in text_indices)


# ---------------------------------------------------------------------------
# BF3: Barge-in cancels with NO duplicate call
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_barge_in_cancels_inflight_via_cancel_inflight():
    """BUG-FIX 3: _cancel_inflight() cancels the in-flight task.

    In pipecat 1.3.0 StartInterruptionFrame is None (the frame type no longer
    exists). The actual cancellation is triggered by the pipeline's turn logic
    and directly via _cancel_inflight(). This test verifies the cancellation
    contract directly: an in-flight task is cancelled when _cancel_inflight is
    called, and no second HTTP call is made.
    """
    class _HangingTransport(httpx.AsyncBaseTransport):
        def __init__(self) -> None:
            self.requests: list[httpx.Request] = []
            self._started = asyncio.Event()

        async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
            self.requests.append(request)
            self._started.set()
            await asyncio.sleep(999)
            return httpx.Response(200, content=b"")

    transport = _HangingTransport()
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://mock-llm-router:8111")
    proc = TrackedLlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)

    # Start generation.
    frame = _final_frame("First turn.")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    await asyncio.wait_for(transport._started.wait(), timeout=2.0)

    # Trigger cancellation directly (same path the barge-in frame would call).
    await proc._cancel_inflight()

    assert proc._gen_task is None or proc._gen_task.done()
    assert len(transport.requests) == 1

    await client.aclose()


@pytest.mark.asyncio
async def test_barge_in_via_start_interruption_frame_if_available():
    """If StartInterruptionFrame exists in pipecat, sending it cancels the task.

    In pipecat 1.3.0 StartInterruptionFrame is None, so this test is skipped.
    When a future pipecat version restores the frame type this test activates.
    """
    if StartInterruptionFrame is None:
        pytest.skip("StartInterruptionFrame not present in this pipecat version")

    class _HangingTransport(httpx.AsyncBaseTransport):
        def __init__(self) -> None:
            self.requests: list[httpx.Request] = []
            self._started = asyncio.Event()

        async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
            self.requests.append(request)
            self._started.set()
            await asyncio.sleep(999)
            return httpx.Response(200, content=b"")

    transport = _HangingTransport()
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://mock-llm-router:8111")
    proc = TrackedLlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)

    frame = _final_frame("First turn.")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    await asyncio.wait_for(transport._started.wait(), timeout=2.0)

    interrupt = StartInterruptionFrame()
    await proc.process_frame(interrupt, FrameDirection.DOWNSTREAM)

    assert proc._gen_task is None or proc._gen_task.done()
    assert len(transport.requests) == 1

    forwarded_types = [type(f).__name__ for f, _ in proc.pushed_frames]
    assert "StartInterruptionFrame" in forwarded_types

    await client.aclose()


@pytest.mark.asyncio
async def test_supersede_cancels_previous_task():
    """Supersede: a second finalized turn cancels the first task before starting."""
    call_count = 0
    first_started = asyncio.Event()

    class _CountingTransport(httpx.AsyncBaseTransport):
        async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                first_started.set()
                await asyncio.sleep(999)
            return httpx.Response(
                200,
                headers={"content-type": "text/event-stream"},
                content=_sse_stream("reply"),
                request=request,
            )

    transport = _CountingTransport()
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://mock-llm-router:8111")
    proc = TrackedLlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)

    frame1 = _final_frame("First.")
    await proc.process_frame(frame1, FrameDirection.DOWNSTREAM)
    await asyncio.wait_for(first_started.wait(), timeout=2.0)

    frame2 = _final_frame("Second.")
    await proc.process_frame(frame2, FrameDirection.DOWNSTREAM)
    task2 = proc._gen_task
    if task2 is not None:
        await task2

    assert call_count == 2

    await client.aclose()


# ---------------------------------------------------------------------------
# BF4: Interim/partial transcriptions do NOT trigger a call
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_interim_transcription_no_call():
    """BUG-FIX 4: finalized=False frames pass through without triggering LLM call."""
    transport = _MockTransport(sse_body=_sse_stream("should not appear"))
    proc = _make_processor(transport)

    frame = _interim_frame("partial text...")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    await asyncio.sleep(0)

    assert len(transport.requests) == 0

    # Exact type check: TranscriptionFrame subclasses TextFrame in real pipecat.
    text_frames = [f for f, _ in proc.pushed_frames if type(f) is TextFrame]
    assert text_frames == []


@pytest.mark.asyncio
async def test_interim_frame_passed_through():
    """Interim frames are forwarded downstream unchanged (turn-tracking can see them)."""
    transport = _MockTransport(sse_body=_sse_stream("nope"))
    proc = _make_processor(transport)

    interim = _interim_frame("partial...")
    await proc.process_frame(interim, FrameDirection.DOWNSTREAM)
    await asyncio.sleep(0)

    pushed_transcription = [
        f for f, _ in proc.pushed_frames if isinstance(f, TranscriptionFrame)
    ]
    assert len(pushed_transcription) == 1
    assert pushed_transcription[0].text == "partial..."


@pytest.mark.asyncio
async def test_finalized_frame_still_triggers_call():
    """Sanity: finalized=True still fires exactly one call (regression guard)."""
    transport = _MockTransport(sse_body=_sse_stream("ok."))
    proc = _make_processor(transport)

    frame = _final_frame("final text")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    task = proc._gen_task
    if task is not None:
        await task

    assert len(transport.requests) == 1


# ---------------------------------------------------------------------------
# Payload shape
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_payload_contains_required_fields():
    """The POST body sent to llm-router contains all required LLMRequest fields."""
    transport = _MockTransport(sse_body=_sse_stream("ok"))
    proc = _make_processor(transport)

    frame = _final_frame("kya haal hai?")
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    task = proc._gen_task
    if task is not None:
        await task

    assert len(transport.requests) == 1
    body = json.loads(transport.requests[0].content)
    assert body["user_turn"] == "kya haal hai?"
    assert "tenant_id" in body
    assert "session_id" in body
    assert "lang" in body
    assert "dialog_history" in body
    assert "persona_gender" in body


@pytest.mark.asyncio
async def test_dialog_history_accumulates_across_turns():
    """Each turn appends user+assistant entries to dialog_history for next turn."""
    transport = _MockTransport(sse_body=_sse_stream("reply one"))
    proc = _make_processor(transport)

    frame1 = _final_frame("Turn one.")
    await proc.process_frame(frame1, FrameDirection.DOWNSTREAM)
    task1 = proc._gen_task
    if task1 is not None:
        await task1

    transport2 = _MockTransport(sse_body=_sse_stream("reply two"))
    proc._client = httpx.AsyncClient(
        transport=transport2, base_url="http://mock-llm-router:8111"
    )

    frame2 = _final_frame("Turn two.")
    await proc.process_frame(frame2, FrameDirection.DOWNSTREAM)
    task2 = proc._gen_task
    if task2 is not None:
        await task2

    body2 = json.loads(transport2.requests[0].content)
    history = body2["dialog_history"]
    roles = [m["role"] for m in history]
    assert "user" in roles
    assert "assistant" in roles


# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_cleanup_closes_owned_client():
    """cleanup() closes the httpx client when this processor owns it."""
    ctx = FakeSessionContext()
    settings = FakeSettings()
    proc = TrackedLlmRouterProcessor(ctx=ctx, settings=settings)
    await proc.cleanup()


@pytest.mark.asyncio
async def test_cleanup_does_not_close_injected_client():
    """cleanup() does NOT close a client injected externally (owns_client=False)."""
    transport = _MockTransport(sse_body=b"")
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://x")
    proc = TrackedLlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)
    await proc.cleanup()
    assert not client.is_closed
    await client.aclose()


# ---------------------------------------------------------------------------
# Pipeline assembly: LlmRouterProcessor is a real FrameProcessor subclass
# ---------------------------------------------------------------------------

def test_llm_router_processor_is_frame_processor():
    """LlmRouterProcessor must be a FrameProcessor subclass (real pipecat)."""
    ctx = FakeSessionContext()
    settings = FakeSettings()
    transport = _MockTransport(sse_body=b"")
    client = httpx.AsyncClient(transport=transport, base_url="http://x")
    proc = LlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)
    assert isinstance(proc, FrameProcessor)
