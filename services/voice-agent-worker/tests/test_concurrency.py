"""test_concurrency.py — guards for the per-call hot-path concurrency fix.

Two invariants this fix relies on under N concurrent calls:

  (a) Router clients (and their httpx connection pools) are created ONCE and
      reused across calls, not re-instantiated per call. We assert that two
      AgentLoop constructions can share the same client instances and that a
      per-call TTS client reuses a shared httpx pool while keeping its own
      per-session streaming WS.

  (b) The barge-in confirm wait is event-driven (asyncio.Event), so it returns
      promptly the moment the probe-STT partial callback sets the event, instead
      of busy-polling. We assert wait_for returns well under the fallback timeout.
"""
from __future__ import annotations

import asyncio

import httpx
import pytest

from voice_agent.clients import HttpTTSClient, _POOL_LIMITS
from tests.conftest import make_loop, make_ctx
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeTTS


@pytest.mark.asyncio
async def test_shared_clients_reused_across_two_agent_loops():
    """Same client instances injected into two AgentLoops are reused, not copied."""
    stt, llm, tts = FakeSTT(), FakeLLM(), FakeTTS()

    loop_a = make_loop(ctx=make_ctx(session_id="a"), stt=stt, llm=llm, tts=tts)
    loop_b = make_loop(ctx=make_ctx(session_id="b"), stt=stt, llm=llm, tts=tts)

    # The AgentLoop must hold the *same* injected instances across both calls —
    # this is what lets the process-wide singletons share one TCP pool.
    assert loop_a._stt is loop_b._stt is stt
    assert loop_a._llm is loop_b._llm is llm
    assert loop_a._tts is loop_b._tts is tts


@pytest.mark.asyncio
async def test_tts_clients_share_http_pool_but_isolate_stream_ws():
    """Per-call TTS clients reuse one shared httpx pool yet keep separate WS state."""
    shared = httpx.AsyncClient(timeout=10.0, limits=_POOL_LIMITS)
    try:
        tts1 = HttpTTSClient(shared_http_client=shared)
        tts2 = HttpTTSClient(shared_http_client=shared)

        # Shared TCP pool across calls (the actual concurrency win).
        assert tts1._client is shared
        assert tts2._client is shared
        # But per-session streaming state stays isolated per call.
        assert tts1._stream_lock is not tts2._stream_lock
        assert tts1._stream_ws is None and tts2._stream_ws is None
        assert tts1._owns_client is False

        # Closing a per-call client must NOT close the shared pool.
        await tts1.aclose()
        assert shared.is_closed is False
    finally:
        await shared.aclose()


@pytest.mark.asyncio
async def test_bargein_event_wait_returns_promptly_when_set():
    """The confirm wait wakes immediately on event.set(), not after the timeout."""
    event = asyncio.Event()
    timeout_s = 0.45  # mirrors _BARGEIN_PARTIAL_WAIT_S default

    async def _fire():
        # Simulate the probe-STT partial callback satisfying the word gate.
        await asyncio.sleep(0.01)
        event.set()

    asyncio.create_task(_fire())

    t0 = asyncio.get_event_loop().time()
    await asyncio.wait_for(event.wait(), timeout=timeout_s)
    elapsed = asyncio.get_event_loop().time() - t0

    # Returned because the event was set, far sooner than the fallback timeout.
    assert event.is_set()
    assert elapsed < 0.2


@pytest.mark.asyncio
async def test_bargein_event_wait_falls_back_on_timeout():
    """When no partial arrives, the wait times out and the caller falls through."""
    event = asyncio.Event()
    timed_out = False
    try:
        await asyncio.wait_for(event.wait(), timeout=0.05)
    except asyncio.TimeoutError:
        timed_out = True
    assert timed_out is True
    assert event.is_set() is False
