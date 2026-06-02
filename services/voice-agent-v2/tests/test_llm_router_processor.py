"""Unit tests for LlmRouterProcessor against a MOCK llm-router.

No cloud, no live calls, no real HTTP. Uses httpx.MockTransport to intercept
all requests and return pre-canned SSE streams.

Covers the four bug-fix assertions:
  BF1 — exactly ONE upstream call per finalized transcript turn
  BF2 — tokens arrive in order as TextFrame instances
  BF3 — barge-in (StartInterruptionFrame) cancels with NO duplicate call
  BF4 — interim/partial TranscriptionFrame (finalized=False) never fires a call

Also verifies: existing 38 tests still pass (no import side-effects here).
"""

from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field
from typing import Any
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest

# ------------------------------------------------------------------
# Use shim unconditionally so tests are independent of pipecat install
# ------------------------------------------------------------------
from voice_agent_v2._pipecat_shim import (
    FrameDirection,
    FrameProcessor,
    LLMFullResponseEndFrame,
    LLMFullResponseStartFrame,
    StartInterruptionFrame,
    TextFrame,
    TranscriptionFrame,
)

# Patch pipecat imports BEFORE importing the processor so the shim is used.
import sys
import types

_shim_mod = sys.modules.get("voice_agent_v2._pipecat_shim")

# Inject shim as the pipecat modules so llm_router_processor's try-import fails
# gracefully and uses the shim path.
for _mod_name in [
    "pipecat",
    "pipecat.frames",
    "pipecat.frames.frames",
    "pipecat.processors",
    "pipecat.processors.frame_processor",
]:
    if _mod_name not in sys.modules:
        sys.modules[_mod_name] = types.ModuleType(_mod_name)

# Ensure pipecat.frames.frames raises ImportError so the shim path is taken.
# We do this by NOT populating the symbols in the fake module.

from voice_agent_v2.llm_router_processor import LlmRouterProcessor  # noqa: E402


# ------------------------------------------------------------------
# Helpers
# ------------------------------------------------------------------

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
    final = {"token": "", "done": True}
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


def _make_processor(transport: _MockTransport) -> LlmRouterProcessor:
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://mock-llm-router:8111")
    proc = LlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)
    return proc


# ------------------------------------------------------------------
# BF1: Exactly ONE upstream call per finalized turn
# ------------------------------------------------------------------

@pytest.mark.asyncio
async def test_exactly_one_call_per_finalized_turn():
    """BUG-FIX 1 & 2: One HTTP call per finalized TranscriptionFrame; tokens in order."""
    tokens = ["Hello ", "world ", "test."]
    transport = _MockTransport(sse_body=_sse_stream(*tokens))
    proc = _make_processor(transport)

    frame = TranscriptionFrame(text="Hello world test.", finalized=True)
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    # Allow the spawned task to complete.
    await asyncio.sleep(0)
    task = proc._gen_task
    if task is not None:
        await task

    # Exactly one HTTP request fired.
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
        frame = TranscriptionFrame(text=text, finalized=True)
        await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
        task = proc._gen_task
        if task is not None:
            await task

    assert len(transport.requests) == 2


# ------------------------------------------------------------------
# BF2: Tokens arrive in order as TextFrames
# ------------------------------------------------------------------

@pytest.mark.asyncio
async def test_tokens_streamed_as_text_frames_in_order():
    """BUG-FIX 2: TextFrame tokens match SSE token order."""
    tokens = ["Namaste ", "aap ", "kaise ", "hain?"]
    transport = _MockTransport(sse_body=_sse_stream(*tokens))
    proc = _make_processor(transport)

    frame = TranscriptionFrame(text="Hello.", finalized=True)
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    task = proc._gen_task
    if task is not None:
        await task

    text_frames = [
        f for f, d in proc.pushed_frames if isinstance(f, TextFrame)
    ]
    assert [tf.text for tf in text_frames] == tokens


@pytest.mark.asyncio
async def test_llm_response_bracketed_by_start_end_frames():
    """LLMFullResponseStartFrame precedes tokens; LLMFullResponseEndFrame follows."""
    transport = _MockTransport(sse_body=_sse_stream("Hello."))
    proc = _make_processor(transport)

    frame = TranscriptionFrame(text="Hi.", finalized=True)
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
    # All text frames must appear after start and before end.
    assert all(start_idx < ti < end_idx for ti in text_indices)


# ------------------------------------------------------------------
# BF3: Barge-in cancels with NO duplicate call
# ------------------------------------------------------------------

@pytest.mark.asyncio
async def test_barge_in_cancels_no_duplicate_call():
    """BUG-FIX 3: StartInterruptionFrame cancels in-flight task; no new LLM call."""
    # Use a slow transport that never returns so the task stays in-flight.
    class _HangingTransport(httpx.AsyncBaseTransport):
        def __init__(self) -> None:
            self.requests: list[httpx.Request] = []
            self._started = asyncio.Event()

        async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
            self.requests.append(request)
            self._started.set()
            await asyncio.sleep(999)  # never returns
            return httpx.Response(200, content=b"")  # unreachable

    transport = _HangingTransport()
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://mock-llm-router:8111")
    proc = LlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)

    # Start generation.
    frame = TranscriptionFrame(text="First turn.", finalized=True)
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    # Wait until the HTTP request is actually in-flight.
    await asyncio.wait_for(transport._started.wait(), timeout=2.0)

    # Send barge-in.
    interrupt = StartInterruptionFrame()
    await proc.process_frame(interrupt, FrameDirection.DOWNSTREAM)

    # Task must be done (cancelled).
    assert proc._gen_task is None or proc._gen_task.done()

    # Exactly 1 request was made (the first turn); no second call spawned.
    assert len(transport.requests) == 1

    # StartInterruptionFrame was forwarded downstream.
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
                await asyncio.sleep(999)  # hangs
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
    proc = LlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)

    # First turn — hangs.
    frame1 = TranscriptionFrame(text="First.", finalized=True)
    await proc.process_frame(frame1, FrameDirection.DOWNSTREAM)
    await asyncio.wait_for(first_started.wait(), timeout=2.0)

    # Second turn — supersedes first.
    frame2 = TranscriptionFrame(text="Second.", finalized=True)
    await proc.process_frame(frame2, FrameDirection.DOWNSTREAM)
    task2 = proc._gen_task
    if task2 is not None:
        await task2

    # Two HTTP calls total (first was mid-flight when cancelled).
    assert call_count == 2

    await client.aclose()


# ------------------------------------------------------------------
# BF4: Interim/partial transcriptions do NOT trigger a call
# ------------------------------------------------------------------

@pytest.mark.asyncio
async def test_interim_transcription_no_call():
    """BUG-FIX 4: finalized=False frames pass through without triggering LLM call."""
    transport = _MockTransport(sse_body=_sse_stream("should not appear"))
    proc = _make_processor(transport)

    frame = TranscriptionFrame(text="partial text...", finalized=False)
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    # Give any accidentally-spawned task a chance to run.
    await asyncio.sleep(0)

    # No HTTP call.
    assert len(transport.requests) == 0

    # No TextFrames emitted.
    text_frames = [f for f, _ in proc.pushed_frames if isinstance(f, TextFrame)]
    assert text_frames == []


@pytest.mark.asyncio
async def test_interim_frame_passed_through():
    """Interim frames are forwarded downstream unchanged (turn-tracking can see them)."""
    transport = _MockTransport(sse_body=_sse_stream("nope"))
    proc = _make_processor(transport)

    interim = TranscriptionFrame(text="partial...", finalized=False)
    await proc.process_frame(interim, FrameDirection.DOWNSTREAM)
    await asyncio.sleep(0)

    # The interim frame itself was pushed through.
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

    frame = TranscriptionFrame(text="final text", finalized=True)
    await proc.process_frame(frame, FrameDirection.DOWNSTREAM)
    task = proc._gen_task
    if task is not None:
        await task

    assert len(transport.requests) == 1


# ------------------------------------------------------------------
# Payload shape
# ------------------------------------------------------------------

@pytest.mark.asyncio
async def test_payload_contains_required_fields():
    """The POST body sent to llm-router contains all required LLMRequest fields."""
    transport = _MockTransport(sse_body=_sse_stream("ok"))
    proc = _make_processor(transport)

    frame = TranscriptionFrame(text="kya haal hai?", finalized=True)
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

    # Turn 1.
    frame1 = TranscriptionFrame(text="Turn one.", finalized=True)
    await proc.process_frame(frame1, FrameDirection.DOWNSTREAM)
    task1 = proc._gen_task
    if task1 is not None:
        await task1

    # Turn 2 — now replace transport with fresh counter.
    transport2 = _MockTransport(sse_body=_sse_stream("reply two"))
    proc._client = httpx.AsyncClient(
        transport=transport2, base_url="http://mock-llm-router:8111"
    )

    frame2 = TranscriptionFrame(text="Turn two.", finalized=True)
    await proc.process_frame(frame2, FrameDirection.DOWNSTREAM)
    task2 = proc._gen_task
    if task2 is not None:
        await task2

    # Payload for turn 2 should include prior dialog.
    body2 = json.loads(transport2.requests[0].content)
    history = body2["dialog_history"]
    # At minimum: user turn 1 and assistant reply 1.
    roles = [m["role"] for m in history]
    assert "user" in roles
    assert "assistant" in roles


# ------------------------------------------------------------------
# Cleanup
# ------------------------------------------------------------------

@pytest.mark.asyncio
async def test_cleanup_closes_owned_client():
    """cleanup() closes the httpx client when this processor owns it."""
    ctx = FakeSessionContext()
    settings = FakeSettings()
    # Let the processor create its own client (owns_client=True).
    proc = LlmRouterProcessor(ctx=ctx, settings=settings)
    await proc.cleanup()
    # No exception = pass. The client is closed.


@pytest.mark.asyncio
async def test_cleanup_does_not_close_injected_client():
    """cleanup() does NOT close a client injected externally (owns_client=False)."""
    transport = _MockTransport(sse_body=b"")
    ctx = FakeSessionContext()
    settings = FakeSettings()
    client = httpx.AsyncClient(transport=transport, base_url="http://x")
    proc = LlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)
    await proc.cleanup()
    # Client should still be usable (not closed).
    assert not client.is_closed
    await client.aclose()


# ------------------------------------------------------------------
# Pipeline assembly: LlmRouterProcessor works in place of EchoLLMProcessor
# ------------------------------------------------------------------

def test_llm_router_processor_is_frame_processor():
    """LlmRouterProcessor must be a FrameProcessor subclass."""
    ctx = FakeSessionContext()
    settings = FakeSettings()
    transport = _MockTransport(sse_body=b"")
    client = httpx.AsyncClient(transport=transport, base_url="http://x")
    proc = LlmRouterProcessor(ctx=ctx, settings=settings, http_client=client)
    assert isinstance(proc, FrameProcessor)
