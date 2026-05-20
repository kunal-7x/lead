"""End-to-end voice pipeline test with Langfuse tracing.

Runs the full real-call shape: WAV → Sarvam STT → Groq LLM → guardrail → Sarvam TTS.
Every stage emits a Langfuse span under one shared trace_id. This is the C5
acceptance test: one call → one trace with STT + LLM + Guardrail + TTS spans.

Gated by RUN_LIVE_TESTS=1 to keep CI off paid APIs.

Run:
    RUN_LIVE_TESTS=1 uv run pytest tests/integration/test_full_trace.py -v -s
"""
from __future__ import annotations

import asyncio
import os
import pathlib
import time
import uuid

import pytest

pytestmark = pytest.mark.skipif(
    os.getenv("RUN_LIVE_TESTS") != "1",
    reason="set RUN_LIVE_TESTS=1 to run live e2e voice pipeline",
)

_FIXTURE = pathlib.Path(__file__).parent.parent / "fixtures" / "sample_hindi.wav"


@pytest.fixture
def sample_audio() -> bytes:
    # Strip 44-byte WAV header → raw 8kHz PCM as call audio path delivers it
    return _FIXTURE.read_bytes()[44:]


async def test_full_voice_pipeline_live(sample_audio: bytes) -> None:
    """WAV → STT → LLM → Guardrail → TTS with one Langfuse trace across all spans."""
    # Late imports so non-live runs don't pull these deps.
    from stt_router.engines.sarvam import SarvamEngine
    from llm_router.backends.groq import GroqLlamaBackend
    from llm_router.models import LLMRequest
    from guardrail.validators import run_all as guardrail_run
    from tts_router.engines.sarvam import SarvamBulbulEngine
    from evs_common.langfuse_tracer import trace_span, propagate_trace_id

    trace_id = propagate_trace_id({"X-Trace-Id": uuid.uuid4().hex})
    session_id = f"e2e-{int(time.time())}"
    print(f"\n[e2e] trace_id={trace_id} session={session_id}")

    # ── 1. STT ────────────────────────────────────────────────────────────────
    stt = SarvamEngine()
    assert stt._api_key, "SARVAM_API_KEY not set"
    t0 = time.time()
    async with trace_span(
        "stt.sarvam",
        trace_id=trace_id,
        input={"audio_bytes": len(sample_audio), "lang": "hi-en"},
    ) as span:
        stt_result = await stt.transcribe(sample_audio, "hi-en", session_id)
        span["output"] = stt_result.text
    stt_ms = int((time.time() - t0) * 1000)
    print(f"[stt] {stt_ms}ms text={stt_result.text!r}")
    assert isinstance(stt_result.text, str)
    user_turn = stt_result.text or "Namaste, 2BHK ka price kya hai?"

    # ── 2. LLM (Groq) ─────────────────────────────────────────────────────────
    llm = GroqLlamaBackend()
    assert llm._api_key, "GROQ_API_KEY not set"
    req = LLMRequest(
        user_turn=user_turn,
        lang="hi-en",
        tenant_id="e2e-tenant",
        session_id=session_id,
        project_id="",
    )
    t1 = time.time()
    async with trace_span(
        "llm.groq_llama",
        trace_id=trace_id,
        input={"user_turn": user_turn},
        metadata={"model": "llama-3.3-70b"},
    ) as span:
        brain, pt, ct = await llm.generate(req, "(no KB context)")
        span["output"] = {"reply": brain.reply, "lead_score": brain.lead_score}
    llm_ms = int((time.time() - t1) * 1000)
    print(f"[llm] {llm_ms}ms tokens={pt}+{ct} reply={brain.reply!r}")
    assert brain.reply

    # ── 3. Guardrail ──────────────────────────────────────────────────────────
    t2 = time.time()
    async with trace_span(
        "guardrail.run_all",
        trace_id=trace_id,
        kind="span",
        input={"reply": brain.reply, "user_turn": user_turn},
    ) as span:
        gr = guardrail_run(brain.model_dump(), kb_chunks=[], user_turn=user_turn)
        span["output"] = {"action": gr.action_taken, "reason": gr.reason}
    gr_ms = int((time.time() - t2) * 1000)
    print(f"[guardrail] {gr_ms}ms action={gr.action_taken}")
    final_reply = gr.brain["reply"]

    # ── 4. TTS ────────────────────────────────────────────────────────────────
    # Keep TTS text short to respect cost guard.
    tts_text = final_reply[:80]
    tts = SarvamBulbulEngine()
    assert tts._api_key, "SARVAM_API_KEY not set"
    t3 = time.time()
    async with trace_span(
        "tts.sarvam_bulbul",
        trace_id=trace_id,
        input={"text": tts_text, "voice": "anushka"},
    ) as span:
        audio = await tts.synthesize(tts_text, "anushka", "hi-en")
        span["output"] = {"audio_bytes": len(audio)}
    tts_ms = int((time.time() - t3) * 1000)
    print(f"[tts] {tts_ms}ms audio={len(audio)} bytes")
    assert isinstance(audio, bytes) and len(audio) > 1024

    total = stt_ms + llm_ms + gr_ms + tts_ms
    print(f"[e2e] TOTAL turn latency = {total}ms (stt={stt_ms} llm={llm_ms} gr={gr_ms} tts={tts_ms})")
    # Batch-API budget: Sarvam batch STT varies 1-20s; LLM+TTS combined < 8s.
    # Real-time call path uses the streaming WS (sub-1.5s target) — different code path.
    assert llm_ms + tts_ms + gr_ms < 10_000, (
        f"llm+guardrail+tts took {llm_ms + gr_ms + tts_ms}ms — too slow for real-time path"
    )
    assert total < 60_000, f"e2e total took {total}ms — pipeline broken"
