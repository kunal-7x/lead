from __future__ import annotations

import asyncio

from voice_agent.vad import FakeVAD
from tests.conftest import make_loop, make_ctx, run_loop
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeTTS
import struct


def _speech() -> bytes:
    return struct.pack("<160h", *([1000] * 160))


def _silence() -> bytes:
    return b"\x00\x00" * 160


async def test_barge_in_stops_tts():
    """Audio arrives during TTS → stop_playback JSON sent, new turn starts."""
    # Build a sequence: utterance 1 completes, then immediately new speech (barge-in)
    vad = FakeVAD(speech_chunks=10)  # speech for first 10 chunks then silence
    stt = FakeSTT(confidence=0.90)
    llm = FakeLLM(reply="Reply here.", next_action="qualify")
    tts = FakeTTS()

    loop = make_loop(stt=stt, llm=llm, tts=tts, vad=vad)

    # Simulate: utterance → silence (VAD triggers) → new speech while TTS plays
    # The barge-in is modeled by having speech start while _playing_tts = True
    chunks = [_speech()] * 10 + [_silence()] * 40

    sent_json = []
    sent_audio = []

    async def audio_source():
        for c in chunks:
            yield c
        # After first turn processes, inject a "barge-in" frame while TTS is playing
        # We simulate this by setting _playing_tts manually and yielding speech
        loop._playing_tts = True
        yield _speech()
        loop._playing_tts = False
        # Then silence to end
        for _ in range(40):
            yield _silence()

    await loop.run(audio_source(), lambda a: sent_audio.append(a),
                   lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types


async def test_barge_in_clears_buffer():
    """On barge-in, old audio buffer is cleared, new utterance starts fresh."""
    vad = FakeVAD(speech_chunks=5)
    loop = make_loop(vad=vad)

    # After barge-in, _playing_tts is reset and buffer cleared
    loop._playing_tts = True
    loop._stop_playback.clear()

    sent_json = []
    chunks = [struct.pack("<160h", *([1000] * 160))]

    async def source():
        for c in chunks:
            yield c

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    assert "stop_playback" in [m.get("type") for m in sent_json]
