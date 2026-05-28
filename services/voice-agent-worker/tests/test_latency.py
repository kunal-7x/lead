from __future__ import annotations

import time
from unittest.mock import patch

from tests.conftest import make_loop, run_loop
from tests.fakes.fake_services import FakeFreeSwitchWS
from voice_agent.agent import _should_flush, _CHUNK_FIRST_WORDS, _CHUNK_MIN_WORDS


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


# ---------------------------------------------------------------------------
# FIX A: first-chunk-small for fast first-audio onset
# ---------------------------------------------------------------------------

def test_first_chunk_flushes_at_comma_after_few_words():
    """First chunk must flush at comma once >=_CHUNK_FIRST_WORDS words are present.

    This is the core of FIX A: a short opener like "नमस्ते, आप 2BHK देख रहे हैं,"
    (5+ words before first comma) should flush immediately as the first chunk,
    not wait for a full sentence-end punctuation. This keeps first TTS chunk tiny
    so Sarvam synthesizes it fast → first-audio <2.5s instead of 5-9s.
    """
    # 5 words ending in comma: should flush as first chunk
    buf_with_comma = "नमस्ते आप 2BHK देख रहे, "
    assert _should_flush(buf_with_comma, is_first_chunk=True), (
        f"First chunk must flush on comma after {_CHUNK_FIRST_WORDS}+ words, "
        f"buf={buf_with_comma!r}"
    )


def test_first_chunk_does_not_flush_below_min_words():
    """First chunk must NOT flush at comma if fewer than _CHUNK_FIRST_WORDS words.

    Prevents a 2-3 word opener ("नमस्ते,") from being sent to TTS as a tiny
    isolated fragment — that would be choppy and unnatural.
    """
    buf_too_short = "नमस्ते, "  # only 1 word before comma
    assert not _should_flush(buf_too_short, is_first_chunk=True), (
        f"First chunk must NOT flush on comma with <{_CHUNK_FIRST_WORDS} words, "
        f"buf={buf_too_short!r}"
    )


def test_subsequent_chunk_does_not_flush_on_comma():
    """Subsequent chunks (not first) must NOT flush at comma — only at sentence-end.

    Only the first chunk uses early comma flush. All subsequent chunks use the
    normal 12-word minimum to avoid choppy audio mid-reply.
    """
    # 8 words ending in comma, but is_first_chunk=False
    buf = "तो मैं आपको बताना चाहूँगा कि इस प्रोजेक्ट, "
    assert not _should_flush(buf, is_first_chunk=False), (
        f"Subsequent chunks must NOT flush on comma (would cause choppy audio), "
        f"buf={buf!r}"
    )


def test_subsequent_chunk_flushes_at_sentence_end_above_min():
    """Subsequent chunks flush at sentence-end once >=_CHUNK_MIN_WORDS words."""
    # 12 words ending in full stop
    buf = "तो मैं आपको बताना चाहूँगा कि इस प्रोजेक्ट में बहुत अच्छे फ्लैट हैं। "
    assert _should_flush(buf, is_first_chunk=False), (
        f"Subsequent chunk must flush at sentence-end with >={_CHUNK_MIN_WORDS} words, "
        f"buf={buf!r}"
    )


def test_first_chunk_flushes_at_sentence_end_too():
    """First chunk also flushes at normal sentence-end (. ! ? ।) with >=5 words."""
    buf = "नमस्ते आप बात करना चाहते हैं। "
    assert _should_flush(buf, is_first_chunk=True), (
        f"First chunk must also flush at sentence-end, buf={buf!r}"
    )
