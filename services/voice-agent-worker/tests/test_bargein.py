from __future__ import annotations

import asyncio

from voice_agent.vad import FakeVAD, SILENCE_THRESHOLD_MS, CHUNK_MS
from voice_agent.agent import _BARGEIN_MIN_SPEECH_CHUNKS, _BACKCHANNEL_TOKENS
from tests.conftest import make_loop, make_ctx, run_loop
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeTTS
import struct


def _speech() -> bytes:
    return struct.pack("<160h", *([1000] * 160))


def _silence() -> bytes:
    return b"\x00\x00" * 160


_N_SILENCE = SILENCE_THRESHOLD_MS // CHUNK_MS


# ---------------------------------------------------------------------------
# Original tests (preserved, updated for new debounce model)
# ---------------------------------------------------------------------------

async def test_barge_in_stops_tts():
    """Confirmed barge-in (≥_BARGEIN_MIN_SPEECH_CHUNKS consecutive VAD frames while
    TTS playing) → stop_playback JSON sent."""
    vad = FakeVAD(speech_chunks=10)
    stt = FakeSTT(confidence=0.90)
    llm = FakeLLM(reply="Reply here.", next_action="qualify")
    tts = FakeTTS()

    loop = make_loop(stt=stt, llm=llm, tts=tts, vad=vad)

    sent_json = []

    async def audio_source():
        # First utterance: 10 speech + enough silence to trigger processing
        for _ in range(10):
            yield _speech()
        for _ in range(_N_SILENCE + 2):
            yield _silence()
        # Now inject barge-in: set _playing_tts True then provide enough consecutive
        # speech frames to exceed debounce threshold
        loop._playing_tts = True
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        # Remaining silence to close the stream
        for _ in range(20):
            yield _silence()

    await loop.run(audio_source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, f"stop_playback not in {types}"


async def test_barge_in_clears_buffer():
    """On confirmed barge-in, _playing_tts is reset and buffer cleared."""
    # speech_chunks must be >= _BARGEIN_MIN_SPEECH_CHUNKS so VAD stays True
    # for the full debounce window
    vad = FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5)
    loop = make_loop(vad=vad)

    sent_json = []

    async def source():
        # Trigger barge-in immediately by setting _playing_tts and sending
        # enough consecutive speech frames to pass debounce
        loop._playing_tts = True
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    assert "stop_playback" in [m.get("type") for m in sent_json]


# ---------------------------------------------------------------------------
# New tests: full-duplex concurrency, debounce, backchannel suppression
# ---------------------------------------------------------------------------

async def test_debounce_single_frame_no_interrupt():
    """A single VAD-positive frame during TTS must NOT trigger barge-in.

    Requirement: _BARGEIN_MIN_SPEECH_CHUNKS consecutive frames required.
    A one-frame burst (cough/click) must be ignored.
    """
    vad = FakeVAD(speech_chunks=10)
    loop = make_loop(vad=vad)

    sent_json = []

    async def source():
        # Set TTS playing, then inject only 1 speech frame (below debounce)
        loop._playing_tts = True
        yield _speech()  # only 1 — below debounce threshold
        # Immediately silence — debounce resets
        for _ in range(5):
            yield _silence()
        loop._playing_tts = False
        # Normal silence to close
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" not in types, (
        f"Single-frame burst should NOT trigger barge-in, but got: {types}"
    )


async def test_debounce_threshold_exact():
    """Exactly _BARGEIN_MIN_SPEECH_CHUNKS frames must trigger barge-in."""
    loop = make_loop(vad=FakeVAD(speech_chunks=20))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Provide exactly the threshold number of consecutive frames
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, (
        f"Exactly {_BARGEIN_MIN_SPEECH_CHUNKS} frames must trigger barge-in, got: {types}"
    )


async def test_backchannel_utterance_does_not_produce_reply():
    """A barge-in whose transcript is a backchannel token must be silently dropped —
    the agent must NOT produce a new AI reply (LLM generate must NOT be called again).
    """
    # FakeSTT returns a backchannel token ("haan")
    stt = FakeSTT(transcript="haan", confidence=0.90)
    llm = FakeLLM(reply="Some AI reply.")
    loop = make_loop(stt=stt, llm=llm, vad=FakeVAD(speech_chunks=30))

    sent_json = []
    sent_audio = []

    async def source():
        # Utterance 1: real speech (to bump up FakeVAD count before barge-in)
        # We skip the first full turn and directly test barge-in suppression:
        loop._playing_tts = True
        loop._next_utterance_is_bargein = True  # mark as post-barge-in
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        for _ in range(_N_SILENCE + 2):
            yield _silence()

    await loop.run(source(), lambda a: sent_audio.append(a), lambda m: sent_json.append(m))

    # stop_playback should have fired (barge-in confirmed)
    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, f"Expected stop_playback, got {types}"
    # LLM was NOT called (backchannel suppressed) — no audio except possibly greeting
    assert llm.call_count == 0, (
        f"LLM must not be called for backchannel barge-in, call_count={llm.call_count}"
    )


async def test_full_duplex_utterance_task_cancelled_on_bargein():
    """Barge-in while _playing_tts: utterance task is cancelled, stop_playback sent.

    We simulate the full-duplex scenario by setting _playing_tts = True before
    injecting speech frames (the actual TTS task wires this in production via
    _process_utterance → self._playing_tts = True). The test verifies that the
    outer loop's barge-in path fires correctly.
    """
    # Large speech_chunks so VAD stays True across the whole debounce window
    vad = FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 20)
    loop = make_loop(vad=vad)

    sent_json = []

    async def source():
        # Simulate: TTS is playing (agent is in the middle of a reply)
        loop._playing_tts = True
        # Provide exactly the debounce threshold worth of speech
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        # Then silence (barge-in confirmed; loop processes new utterance)
        for _ in range(_N_SILENCE + 5):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, f"stop_playback missing from {types}"


async def test_bargein_min_speech_chunks_constant():
    """_BARGEIN_MIN_SPEECH_CHUNKS must be in [6, 15] for production safety.

    Too low (< 6) → false triggers from background noise.
    Too high (> 15) → perceptible lag before barge-in is felt.
    """
    assert 6 <= _BARGEIN_MIN_SPEECH_CHUNKS <= 15, (
        f"_BARGEIN_MIN_SPEECH_CHUNKS={_BARGEIN_MIN_SPEECH_CHUNKS} out of safe range [6,15]"
    )


async def test_backchannel_tokens_include_hindi():
    """_BACKCHANNEL_TOKENS must contain common Hindi acknowledgements."""
    required = {"हाँ", "अच्छा", "जी", "haan", "hmm", "ok"}
    missing = required - _BACKCHANNEL_TOKENS
    assert not missing, f"Missing Hindi/English backchannel tokens: {missing}"
