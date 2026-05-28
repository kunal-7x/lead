from __future__ import annotations

import time
from unittest.mock import patch

from tests.conftest import make_loop, run_loop
from tests.fakes.fake_services import FakeFreeSwitchWS


async def test_pipeline_overhead():
    """With fake services returning instantly, pipeline overhead < 500ms per turn.

    Uses a tiny filler gap so the asyncio.wait_for timeout doesn't pad the
    measurement — this test checks pipeline logic overhead, not filler timing.
    """
    import voice_agent.agent as agent_mod

    loop = make_loop()
    ws = FakeFreeSwitchWS()

    with patch.object(agent_mod, "_FILLER_GAP_MS", 50.0):
        t0 = time.perf_counter()
        await run_loop(loop, ws.all_chunks())
        elapsed_ms = (time.perf_counter() - t0) * 1000

    # Single turn through fake pipeline should be very fast
    assert elapsed_ms < 500, f"Pipeline took {elapsed_ms:.1f}ms (expected <500ms)"


async def test_multiple_turns_reasonable_time():
    """5 turns with fake services complete in reasonable time."""
    from voice_agent.vad import FakeVAD
    vad = FakeVAD(speech_chunks=10)
    loop = make_loop(vad=vad)
    ws = FakeFreeSwitchWS()
    chunks = ws.all_chunks() * 5

    t0 = time.perf_counter()
    await run_loop(loop, chunks)
    elapsed_ms = (time.perf_counter() - t0) * 1000

    assert elapsed_ms < 2000, f"5 turns took {elapsed_ms:.1f}ms"
