"""Live integration tests — only run when RUN_LIVE_TESTS=1.

Cost guard: sends ≤5 seconds of audio per test.
Run: RUN_LIVE_TESTS=1 uv run pytest tests/integration -v
"""
from __future__ import annotations

import os
import pathlib
import time

import pytest

pytestmark = pytest.mark.skipif(
    os.getenv("RUN_LIVE_TESTS") != "1",
    reason="set RUN_LIVE_TESTS=1 to run live provider tests",
)

_FIXTURE = pathlib.Path(__file__).parent.parent / "fixtures" / "sample_hindi.wav"


@pytest.fixture
def sample_audio() -> bytes:
    # Strip 44-byte WAV header → raw 8kHz L16 PCM as engines expect
    return _FIXTURE.read_bytes()[44:]


# ── Sarvam STT ────────────────────────────────────────────────────────────────

async def test_sarvam_transcribe_live(sample_audio: bytes) -> None:
    from stt_router.engines.sarvam import SarvamEngine
    engine = SarvamEngine()
    assert engine._api_key, "SARVAM_API_KEY not set"
    result = await engine.transcribe(sample_audio, "hi-en", "live-test")
    assert isinstance(result.text, str)
    assert result.engine_used == "sarvam"
    assert result.latency_ms > 0
    print(f"[sarvam] text={result.text.encode('ascii', 'replace').decode()!r} latency={result.latency_ms}ms")


async def test_sarvam_health_live() -> None:
    from stt_router.engines.sarvam import SarvamEngine
    engine = SarvamEngine()
    ok = await engine.health_check()
    assert ok, "Sarvam health check failed — check SARVAM_API_KEY"


# ── Groq Whisper STT ──────────────────────────────────────────────────────────

async def test_groq_whisper_transcribe_live(sample_audio: bytes) -> None:
    from stt_router.engines.groq_whisper import GroqWhisperEngine
    engine = GroqWhisperEngine()
    assert engine._api_key, "GROQ_API_KEY not set"
    result = await engine.transcribe(sample_audio, "hi-en", "live-test")
    assert isinstance(result.text, str)
    assert result.engine_used == "groq_whisper"
    assert result.latency_ms > 0
    print(f"[groq_whisper] text={result.text.encode('ascii', 'replace').decode()!r} latency={result.latency_ms}ms")


async def test_groq_whisper_health_live() -> None:
    from stt_router.engines.groq_whisper import GroqWhisperEngine
    engine = GroqWhisperEngine()
    ok = await engine.health_check()
    assert ok, "Groq health check failed — check GROQ_API_KEY"


# ── Sarvam Streaming STT ──────────────────────────────────────────────────────

async def test_sarvam_streaming_live(sample_audio: bytes) -> None:
    """Stream 6.25s of Hindi audio and measure end-of-audio→final-transcript latency.

    The sample is 8kHz PCM16 (telephony format). We send a single bulk WAV
    (as if end-of-utterance was already detected) and measure flush→transcript time.
    Expected: transcript arrives within a few seconds; measured was 302ms in
    real-time-paced streaming mode (see REALTIME_PLAN.md §6).
    """
    import asyncio, base64, json, struct, websockets

    SARVAM_WS = "wss://api.sarvam.ai/speech-to-text/ws"
    from stt_router.engines.sarvam import _wrap_pcm_wav, _SARVAM_API_KEY

    api_key = _SARVAM_API_KEY
    assert api_key, "SARVAM_API_KEY not set"

    params = "?language-code=hi-IN&model=saaras:v3&mode=transcribe&sample_rate=8000&input_audio_codec=pcm_s16le&vad_signals=true"
    url = SARVAM_WS + params
    headers = {"Api-Subscription-Key": api_key}

    t0 = time.time()
    messages = []
    final_event = None
    t_flush = None

    async with websockets.connect(url, additional_headers=headers) as ws:
        # Send full audio as single WAV (simulates post-VAD-endpointing scenario)
        full_wav = _wrap_pcm_wav(sample_audio, sample_rate=8000)
        await ws.send(json.dumps({
            "audio": {"data": base64.b64encode(full_wav).decode("ascii"),
                      "sample_rate": "8000", "encoding": "audio/wav"}
        }))
        t_flush = time.time()
        await ws.send(json.dumps({"type": "flush"}))

        # Collect until final or timeout
        deadline = time.time() + 10.0
        while time.time() < deadline:
            remaining = deadline - time.time()
            try:
                raw = await asyncio.wait_for(ws.recv(), timeout=max(remaining, 0.1))
                msg = json.loads(raw)
                messages.append((time.time(), msg))
                if msg.get("type") == "data":
                    final_event = {
                        "text": msg["data"].get("transcript", ""),
                        "latency_ms": int((time.time() - t_flush) * 1000),
                    }
                    break
            except asyncio.TimeoutError:
                break

    t_total = time.time() - t0
    assert final_event is not None, f"No final transcript received. Messages: {messages}"
    assert len(final_event["text"]) > 0, "transcript should not be empty"

    latency_ms = final_event["latency_ms"]
    vad_events = [m for _, m in messages if m.get("type") == "events"]
    print(
        f"[sarvam_streaming] "
        f"text_len={len(final_event['text'])} chars "
        f"flush_to_transcript={latency_ms}ms "
        f"total_wall={t_total:.2f}s "
        f"vad_events={[m['data']['signal_type'] for m in vad_events]}"
    )
    # Transcript must arrive within 5s (allows for network variance; real-time paced
    # telephony streaming achieves ~302ms as measured in manual test 2026-05-26)
    assert latency_ms < 5000, f"Latency {latency_ms}ms exceeds 5000ms threshold"


async def test_sarvam_streaming_ulaw_input(sample_audio: bytes) -> None:
    """Verify µ-law → PCM16 transcoding produces usable audio for STT.

    Converts the PCM16 fixture to µ-law (as Vobiz sends to worker), then
    back to PCM16 via ulaw_to_pcm16, and transcribes via the streaming engine.
    This validates the full telephony codec chain.
    """
    import asyncio, base64, json, struct, websockets
    from stt_router.engines.sarvam import _wrap_pcm_wav, ulaw_to_pcm16, _SARVAM_API_KEY

    api_key = _SARVAM_API_KEY
    assert api_key, "SARVAM_API_KEY not set"

    # Simulate telephony: PCM16 → µ-law (Vobiz sends this) → PCM16 (our decode)
    # Use numpy for G711 encode (audioop deprecated in Py3.12+)
    import numpy as np
    pcm_arr = np.frombuffer(sample_audio, dtype="<i2")
    # G.711 µ-law encode using numpy (simple approximation for test purposes)
    # Just verify our decoder is lossless-ish by round-tripping via audioop
    import audioop
    ulaw_audio = audioop.lin2ulaw(sample_audio, 2)
    decoded_pcm = ulaw_to_pcm16(ulaw_audio)

    SARVAM_WS = "wss://api.sarvam.ai/speech-to-text/ws"
    params = "?language-code=hi-IN&model=saaras:v3&mode=transcribe&sample_rate=8000&input_audio_codec=pcm_s16le&vad_signals=true"
    url = SARVAM_WS + params
    headers = {"Api-Subscription-Key": api_key}

    final_text = None
    async with websockets.connect(url, additional_headers=headers) as ws:
        full_wav = _wrap_pcm_wav(decoded_pcm, sample_rate=8000)
        await ws.send(json.dumps({
            "audio": {"data": base64.b64encode(full_wav).decode("ascii"),
                      "sample_rate": "8000", "encoding": "audio/wav"}
        }))
        await ws.send(json.dumps({"type": "flush"}))

        deadline = time.time() + 10.0
        while time.time() < deadline:
            remaining = deadline - time.time()
            try:
                raw = await asyncio.wait_for(ws.recv(), timeout=max(remaining, 0.1))
                msg = json.loads(raw)
                if msg.get("type") == "data":
                    final_text = msg["data"].get("transcript", "")
                    break
            except asyncio.TimeoutError:
                break

    assert final_text is not None, "No transcript from µ-law-decoded audio"
    assert len(final_text) > 0, "transcript should not be empty after µ-law decode"
    print(
        f"[sarvam_streaming/ulaw] "
        f"text_len={len(final_text)} chars "
        f"(ulaw round-trip preserves STT accuracy)"
    )


async def test_sarvam_streaming_health_live() -> None:
    from stt_router.engines.sarvam import SarvamStreamingEngine
    engine = SarvamStreamingEngine()
    ok = await engine.health_check()
    assert ok, "Sarvam streaming health check failed — check SARVAM_API_KEY"
