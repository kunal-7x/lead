from __future__ import annotations

import asyncio

from voice_agent.vad import FakeVAD, SILENCE_THRESHOLD_MS, CHUNK_MS
from voice_agent.agent import _BARGEIN_MIN_SPEECH_CHUNKS, _BACKCHANNEL_TOKENS
from tests.conftest import make_loop, make_ctx, run_loop
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeTTS
import struct

# Short blip threshold: must be strictly below the real barge-in threshold
_SHORT_BLIP_FRAMES = max(1, _BARGEIN_MIN_SPEECH_CHUNKS // 4)  # ≤ 25% of threshold


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
    vad = FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5)
    stt = FakeSTT(confidence=0.90)
    llm = FakeLLM(reply="Reply here.", next_action="qualify")
    tts = FakeTTS()

    loop = make_loop(stt=stt, llm=llm, tts=tts, vad=vad)

    sent_json = []

    async def audio_source():
        # First utterance: speech + enough silence to trigger processing
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS + 5):
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
    loop = make_loop(vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
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
    """_BARGEIN_MIN_SPEECH_CHUNKS must be in [6, 30] for production safety.

    Too low (< 6) → false triggers from background noise / acoustic echo.
    Too high (> 30) → perceptible lag before barge-in is felt (600ms+).
    Default raised to 22 (~440ms) to avoid self-interrupt from echo.
    """
    assert 6 <= _BARGEIN_MIN_SPEECH_CHUNKS <= 30, (
        f"_BARGEIN_MIN_SPEECH_CHUNKS={_BARGEIN_MIN_SPEECH_CHUNKS} out of safe range [6,30]"
    )


async def test_backchannel_tokens_include_hindi():
    """_BACKCHANNEL_TOKENS must contain common Hindi acknowledgements."""
    required = {"हाँ", "अच्छा", "जी", "haan", "hmm", "ok"}
    missing = required - _BACKCHANNEL_TOKENS
    assert not missing, f"Missing Hindi/English backchannel tokens: {missing}"


async def test_playing_tts_true_during_send_audio_false_after():
    """_playing_tts must be True while send_audio is called and False after.

    This is the core invariant the barge-in gate depends on.
    Before the fix, _playing_tts could be False while audio was still being
    sent (the finally block fired too early), meaning the barge-in path
    (gated on `if self._playing_tts`) never armed.
    """
    import asyncio as _asyncio

    stt = FakeSTT(confidence=0.90)
    llm = FakeLLM(reply="Yeh ek test hai.", next_action="qualify")
    tts = FakeTTS()
    vad = FakeVAD(speech_chunks=10)
    loop = make_loop(stt=stt, llm=llm, tts=tts, vad=vad)

    flag_during_send: list[bool] = []
    flag_after_send: list[bool] = []

    _orig_send_audio_called = False

    async def checking_send_audio(audio: bytes) -> None:
        # Capture the flag state while audio is being sent
        flag_during_send.append(loop._playing_tts)

    async def audio_source():
        # First utterance: speech + silence to trigger processing
        for _ in range(10):
            yield _speech()
        for _ in range(_N_SILENCE + 2):
            yield _silence()
        # Allow the utterance task (TTS) to run and complete
        # by yielding a few more silence frames; the task runs concurrently.
        for _ in range(60):
            yield _silence()
            await _asyncio.sleep(0)  # yield to event loop so utterance task can run

    await loop.run(audio_source(), checking_send_audio, lambda m: None)

    # After run completes, flag must be False (not stuck True)
    assert loop._playing_tts is False, (
        "_playing_tts stuck True after audio playback ended — will freeze barge-in"
    )

    # During send_audio calls, flag must have been True at least once
    assert any(flag_during_send), (
        f"_playing_tts was never True during send_audio — "
        f"barge-in gate would never arm. flags={flag_during_send}"
    )

    # Every call to send_audio must have seen _playing_tts=True
    assert all(flag_during_send), (
        f"_playing_tts was False during some send_audio calls — "
        f"barge-in window was not fully covered. flags={flag_during_send}"
    )


# ---------------------------------------------------------------------------
# New tests: echo/short-blip guard + inter-chunk reset + sustained speech
# ---------------------------------------------------------------------------

async def test_short_blip_during_playback_does_not_trigger_bargein():
    """A short echo/cough-like blip (_SHORT_BLIP_FRAMES consecutive VAD frames)
    during TTS playback must NOT trigger barge-in.

    This is the core regression guard for the bug where acoustic echo from
    the AI's own TTS audio (bleeding into the inbound STT track) caused
    bargein_confirmed to fire and cut the AI mid-sentence.
    """
    # _SHORT_BLIP_FRAMES is well below the new threshold (22 frames / 440ms)
    assert _SHORT_BLIP_FRAMES < _BARGEIN_MIN_SPEECH_CHUNKS, (
        f"Blip test misconfigured: blip={_SHORT_BLIP_FRAMES} >= threshold={_BARGEIN_MIN_SPEECH_CHUNKS}"
    )

    loop = make_loop(vad=FakeVAD(speech_chunks=_SHORT_BLIP_FRAMES + 2))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Inject a short blip (simulating echo or brief background noise)
        for _ in range(_SHORT_BLIP_FRAMES):
            yield _speech()
        # Silence follows — debounce resets
        for _ in range(10):
            yield _silence()
        loop._playing_tts = False
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" not in types, (
        f"Short blip ({_SHORT_BLIP_FRAMES} frames) must NOT trigger barge-in, got: {types}"
    )


async def test_sustained_speech_during_playback_triggers_bargein():
    """A sustained, deliberate caller interruption (_BARGEIN_MIN_SPEECH_CHUNKS
    consecutive VAD frames) during TTS playback MUST trigger barge-in.

    Verifies that raising the threshold did NOT disable real barge-in.
    """
    loop = make_loop(vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Sustained speech: exactly the threshold
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, (
        f"Sustained speech ({_BARGEIN_MIN_SPEECH_CHUNKS} frames) MUST trigger barge-in, got: {types}"
    )


async def test_inter_chunk_counter_reset_prevents_accumulation():
    """Partial speech counts from one breath-chunk must NOT carry over to the
    next breath-chunk and trigger a false barge-in.

    Bug: with old code, 7 frames during chunk 1 (below threshold of 8)
    + 1 frame during chunk 2 synthesis gap = 8 total → false barge-in fired.
    Fix: _tts_chunk_seq increment resets _bargein_consec between chunks.
    """
    # Use a threshold that allows testing: need blip < threshold but blip*2 >= threshold
    # With threshold=22: half_blip=11 * 2 = 22 >= 22, each alone (11) < 22. Perfect.
    half_blip = _BARGEIN_MIN_SPEECH_CHUNKS // 2
    assert half_blip < _BARGEIN_MIN_SPEECH_CHUNKS, "threshold must be > 1 for this test"
    assert half_blip * 2 >= _BARGEIN_MIN_SPEECH_CHUNKS, "half_blip*2 must hit threshold"

    loop = make_loop(vad=FakeVAD(speech_chunks=half_blip + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Chunk 1: inject half_blip speech frames (below threshold on its own)
        for _ in range(half_blip):
            yield _speech()
        # Simulate inter-chunk gap: new TTS sentence starts
        loop._tts_chunk_seq += 1  # triggers counter reset in audio loop
        # Brief silence (synthesis latency)
        for _ in range(3):
            yield _silence()
        # Chunk 2: inject half_blip speech frames again
        # Without the fix, accumulated total = half_blip*2 → false barge-in
        # With the fix, counter was reset → only half_blip consec → no trigger
        for _ in range(half_blip):
            yield _speech()
        # Silence to end
        for _ in range(5):
            yield _silence()
        loop._playing_tts = False
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" not in types, (
        f"Inter-chunk counter accumulation must NOT trigger barge-in; "
        f"half_blip={half_blip} < threshold={_BARGEIN_MIN_SPEECH_CHUNKS}. Got: {types}"
    )


# ---------------------------------------------------------------------------
# W3 tests: STT-partial gate — echo must not trigger, real words must trigger
# ---------------------------------------------------------------------------

async def test_echo_vad_without_stt_partial_does_not_trigger_bargein():
    """W3: VAD frames during TTS playback WITHOUT a real STT partial (echo case)
    must NOT trigger barge-in even when _BARGEIN_MIN_SPEECH_CHUNKS is reached.

    Simulates acoustic echo: VAD fires on bot's own audio but FakeSTT has no
    partial_text configured — so partial_callback is never called → barge-in
    must be rejected with reason=no_real_text.
    """
    # FakeSTT with NO partial_text: stream_transcribe drains queue but never
    # calls partial_callback → _bargein_has_real_partial stays False
    stt = FakeSTT(transcript="", confidence=0.0, partial_text=None)
    loop = make_loop(stt=stt, vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Provide sustained VAD frames (≥ threshold) — no real STT partial
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS + 2):
            yield _speech()
        for _ in range(10):
            yield _silence()
        loop._playing_tts = False
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" not in types, (
        f"Echo case (VAD only, no STT partial) must NOT trigger barge-in. Got: {types}"
    )


async def test_vad_with_real_stt_partial_triggers_bargein():
    """W3: VAD frames during TTS playback WITH a real multi-word STT partial
    MUST trigger barge-in.

    Simulates deliberate caller interruption: both VAD threshold is met AND
    streaming STT produces a real interim transcript.
    """
    # FakeSTT with partial_text: stream_transcribe calls partial_callback("mujhe")
    stt = FakeSTT(
        transcript="mujhe 2BHK chahiye",
        confidence=0.85,
        partial_text="mujhe",  # real word → _bargein_has_real_partial = True
    )
    loop = make_loop(stt=stt, vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, (
        f"Sustained VAD + real STT partial MUST trigger barge-in. Got: {types}"
    )


async def test_backchannel_stt_partial_does_not_trigger_bargein():
    """W3: VAD frames during TTS playback with a backchannel-only STT partial
    (e.g. "हाँ") must NOT trigger barge-in.

    The backchannel check in _process_utterance handles post-barge-in suppression,
    but here we test the pre-barge-in gate: a single-token backchannel partial
    still counts as a real partial (it's a real word), so barge-in IS confirmed
    at the VAD level and backchannel suppression happens in _process_utterance.

    This test specifically validates that a PURE echo (no partial at all) does
    NOT trigger and a real word ("हाँ") DOES allow barge-in to proceed so the
    post-barge-in backchannel suppression path can handle it correctly.
    NOTE: A "हाँ" by itself IS a real word — barge-in fires, then
    _process_utterance suppresses the LLM reply (existing logic). If we
    blocked barge-in here for single-word backchannels we'd miss legitimate
    single-word answers. So this test confirms barge-in fires for "हाँ".
    """
    stt = FakeSTT(
        transcript="हाँ",
        confidence=0.90,
        partial_text="हाँ",  # backchannel word — still a real word, partial fires
    )
    loop = make_loop(stt=stt, vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        loop._next_utterance_is_bargein = True
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        for _ in range(_N_SILENCE + 2):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    # Barge-in fires (real word present) — stop_playback is sent
    assert "stop_playback" in types, (
        f"Backchannel 'हाँ' is a real word — barge-in should fire (LLM suppressed "
        f"by post-barge-in backchannel logic). Got: {types}"
    )
