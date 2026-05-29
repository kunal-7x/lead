from __future__ import annotations

import asyncio

from voice_agent.vad import FakeVAD, SILENCE_THRESHOLD_MS, CHUNK_MS
from voice_agent.agent import (
    _BARGEIN_MIN_SPEECH_CHUNKS,
    _BACKCHANNEL_TOKENS,
    _BARGEIN_ECHO_ENERGY_FLOOR,
    _BARGEIN_MIN_PARTIAL_WORDS,
)
from tests.conftest import make_loop, make_ctx, run_loop
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeTTS
import struct

# Short blip threshold: must be strictly below the real barge-in threshold
_SHORT_BLIP_FRAMES = max(1, _BARGEIN_MIN_SPEECH_CHUNKS // 4)  # ≤ 25% of threshold


def _speech() -> bytes:
    # RMS 1000 — clearly above the echo-energy floor → counts as loud speech.
    return struct.pack("<160h", *([1000] * 160))


def _quiet_echo() -> bytes:
    """Low-energy frame: VAD may flag it as speech, but RMS is BELOW the echo
    floor — models residual echo of the bot's own TTS bleeding into the inbound
    track. Must NOT count toward the barge-in debounce."""
    amp = int(_BARGEIN_ECHO_ENERGY_FLOOR / 2)  # well under the floor
    return struct.pack("<160h", *([amp] * 160))


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
    """_BARGEIN_MIN_SPEECH_CHUNKS must be in [6, 25] for production safety.

    Too low (< 6) → false triggers from background noise / acoustic echo.
    Too high (> 25) → deliberate "रुको रुको" misses — perceptible 500ms+ lag.
    Lowered default to 16 (~320ms) so real interruptions fire within 300-500ms
    while echo (no STT partial text) is still blocked by the STT gate.
    """
    assert 6 <= _BARGEIN_MIN_SPEECH_CHUNKS <= 25, (
        f"_BARGEIN_MIN_SPEECH_CHUNKS={_BARGEIN_MIN_SPEECH_CHUNKS} out of safe range [6,25]"
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


async def test_speech_through_chunk_boundary_still_accumulates():
    """T1.3 REGRESSION FIX: a caller speaking CONTINUOUSLY across a TTS breath-
    chunk boundary (_tts_chunk_seq bump) must keep accumulating and confirm.

    THIS is the bug that broke barge-in in the field: the old code zeroed
    _bargein_consec AND cancelled the probe STT on every _tts_chunk_seq bump
    (every ~2-6s), so a caller talking through a boundary never reached the
    threshold and barge-in never fired ("रुक जाओ" 7× ignored). The fix only
    resets on genuine VAD silence, never on a chunk bump.

    Here the caller speaks half the threshold, a new TTS chunk starts mid-speech
    (seq bump, NO silence), then speaks the other half — total >= threshold WITH
    a real >=2-word partial → barge-in MUST confirm.
    """
    half = _BARGEIN_MIN_SPEECH_CHUNKS // 2 + 1  # two halves comfortably exceed threshold
    # Default FakeSTT partial_text="test speech" → 2 words → passes the word gate
    loop = make_loop(vad=FakeVAD(speech_chunks=half * 2 + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # First half of a continuous interruption
        for _ in range(half):
            yield _speech()
        # New TTS breath-chunk starts WHILE the caller is still speaking. No
        # silence frame — this is mid-word. Old code reset here; new code must not.
        loop._tts_chunk_seq += 1
        # Second half — speech continues uninterrupted
        for _ in range(half):
            yield _speech()
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, (
        f"Continuous speech through a chunk boundary MUST accumulate and confirm "
        f"barge-in (the field regression). half={half}, threshold="
        f"{_BARGEIN_MIN_SPEECH_CHUNKS}. Got: {types}"
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
        # >=2 real words → deliberate interruption (T1.3 gate: lone words don't fire)
        partial_text="mujhe 2BHK",
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
    """T1.3: a LONE backchannel partial ("हाँ") during TTS playback must NOT
    trigger barge-in.

    The barge-in confirm gate requires >= _BARGEIN_MIN_PARTIAL_WORDS (2) real
    words. A single-token backchannel ("हाँ"/"hmm"/"ok") is the caller saying
    "I hear you, keep going" — it is exactly 1 word, so it is rejected at the
    gate (reason=too_short) and the AI keeps talking. This is the field
    requirement: a lone "हाँ" while the bot speaks does not cut it off.
    """
    stt = FakeSTT(
        transcript="हाँ",
        confidence=0.90,
        partial_text="हाँ",  # 1 word backchannel — below the 2-word confirm gate
    )
    loop = make_loop(stt=stt, vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        loop._next_utterance_is_bargein = True
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS + 4):
            yield _speech()
        for _ in range(_N_SILENCE + 2):
            yield _silence()
        loop._playing_tts = False
        for _ in range(10):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    # Lone backchannel ("हाँ" = 1 word) must NOT interrupt the AI.
    assert "stop_playback" not in types, (
        f"Lone backchannel 'हाँ' (1 word) must NOT trigger barge-in. Got: {types}"
    )


# ---------------------------------------------------------------------------
# FIX B tests: lowered threshold (22→16) + no_real_text back-off (not hard reset)
# ---------------------------------------------------------------------------

async def test_bargein_fires_with_real_partial_within_window():
    """FIX B: deliberate 'रुको रुको' — VAD sustained >= threshold AND real STT
    partial present — must fire barge-in within ~320ms (16 frames at 20ms each).

    With the old 22-frame threshold + hard-reset on no_real_text, fragmented
    speech like 'रु-को' could miss the partial window and never confirm. Now:
    - threshold lowered to 16 (~320ms)
    - probe STT gets 8 async yield ticks (was 3) to return a partial
    - Result: deliberate interruption confirmed within 300-500ms.
    """
    stt = FakeSTT(
        transcript="रुको रुको",
        confidence=0.85,
        partial_text="रुको रुको",  # >=2 real Hindi words arrive in probe stream
    )
    loop = make_loop(stt=stt, vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Provide exactly the (new, lower) threshold of sustained speech
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS):
            yield _speech()
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" in types, (
        f"Deliberate 'रुको रुको' with real STT partial MUST trigger barge-in within "
        f"{_BARGEIN_MIN_SPEECH_CHUNKS} frames (~{_BARGEIN_MIN_SPEECH_CHUNKS * 20}ms). "
        f"Got: {types}"
    )


async def test_echo_without_stt_partial_still_rejected_after_threshold_lower():
    """FIX B regression: lowering threshold to 16 must NOT enable echo barge-in.

    Echo has no STT partial text → _bargein_has_real_partial stays False →
    barge-in must be rejected even though VAD threshold is easier to reach.
    The STT gate is the essential guard against echo self-interrupt.
    """
    stt = FakeSTT(transcript="", confidence=0.0, partial_text=None)  # no partial
    loop = make_loop(stt=stt, vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 5))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Sustain VAD above new (lower) threshold — but no STT text
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS + 4):
            yield _speech()
        for _ in range(10):
            yield _silence()
        loop._playing_tts = False
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" not in types, (
        f"Echo (VAD only, no STT partial) must NOT trigger barge-in even with "
        f"lowered threshold={_BARGEIN_MIN_SPEECH_CHUNKS}. Got: {types}"
    )


# ---------------------------------------------------------------------------
# T1.3 tests: energy gate + 2-word confirm + through-boundary regression
# ---------------------------------------------------------------------------

async def test_low_energy_echo_frames_do_not_trigger_bargein():
    """T1.3 energy gate: VAD-positive frames that sit BELOW the echo-energy
    floor (residual echo of the bot's own TTS) must NOT count toward barge-in,
    even with a real STT partial configured.

    This guards against the bot interrupting itself on its own audio echo.
    """
    # Real 2-word partial is configured — proving the rejection is the ENERGY
    # gate, not the word gate.
    stt = FakeSTT(transcript="रुको रुको", confidence=0.85, partial_text="रुको रुको")
    # FakeVAD flags every frame as speech for the whole window.
    loop = make_loop(stt=stt, vad=FakeVAD(speech_chunks=_BARGEIN_MIN_SPEECH_CHUNKS + 20))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Plenty of VAD-positive frames, but all BELOW the energy floor (echo).
        for _ in range(_BARGEIN_MIN_SPEECH_CHUNKS + 8):
            yield _quiet_echo()
        for _ in range(10):
            yield _silence()
        loop._playing_tts = False
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    types = [m.get("type") for m in sent_json]
    assert "stop_playback" not in types, (
        f"Low-energy echo frames must NOT trigger barge-in (energy gate). Got: {types}"
    )


async def test_min_partial_words_constant():
    """A lone backchannel is 1 word; a deliberate interruption is >=2. The
    confirm gate must require at least 2 so single tokens never cut off the AI."""
    assert _BARGEIN_MIN_PARTIAL_WORDS >= 2, (
        f"_BARGEIN_MIN_PARTIAL_WORDS={_BARGEIN_MIN_PARTIAL_WORDS} must be >= 2 "
        f"so lone backchannels don't interrupt"
    )


# ---------------------------------------------------------------------------
# Energy-delta-above-echo-baseline barge-in (the telephony-line fix).
# On a real phone line the bot's TTS echoes into the inbound track, so the probe
# STT returns 0 words during playback. These tests model that: a STEADY echo
# level (caller silent) that must NOT fire, and a caller spike clearly ABOVE the
# echo baseline that MUST fire — WITHOUT any STT words.
# ---------------------------------------------------------------------------

from voice_agent.agent import (  # noqa: E402
    _BARGEIN_DELTA_SUSTAIN_FRAMES,
    _BARGEIN_DELTA_MULT,
    _BARGEIN_DELTA_ABS_MARGIN,
    _BARGEIN_ENERGY_DELTA_MODE,
)

# A "no STT words" STT — models the echo line where probe STT can't transcribe.
_ECHO_LINE_STT = lambda: FakeSTT(transcript="", confidence=0.0, partial_text=None)

_ECHO_RMS = 800  # steady echo baseline level


def _frame(rms: int) -> bytes:
    return struct.pack("<160h", *([rms] * 160))


def _echo_steady() -> bytes:
    """Steady residual echo of the bot's own TTS (caller silent)."""
    return _frame(_ECHO_RMS)


def _caller_over_echo() -> bytes:
    """Caller speaking ON TOP of the echo: energy clearly above the adaptive
    baseline bar = max(baseline*MULT, baseline+ABS_MARGIN)."""
    bar = max(_ECHO_RMS * _BARGEIN_DELTA_MULT, _ECHO_RMS + _BARGEIN_DELTA_ABS_MARGIN)
    return _frame(int(bar) + 800)


async def test_energy_delta_mode_default_on():
    assert _BARGEIN_ENERGY_DELTA_MODE, "energy-delta barge-in must default ON"


async def test_steady_echo_level_does_not_trigger_bargein():
    """Steady echo (caller silent) at the baseline level must NEVER fire, even
    sustained well past the sustain window and with NO STT words."""
    loop = make_loop(stt=_ECHO_LINE_STT(),
                     vad=FakeVAD(speech_chunks=_BARGEIN_DELTA_SUSTAIN_FRAMES * 4))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # Long run of steady echo — caller is silent.
        for _ in range(_BARGEIN_DELTA_SUSTAIN_FRAMES * 3):
            yield _echo_steady()
        loop._playing_tts = False
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    assert "stop_playback" not in [m.get("type") for m in sent_json], (
        "Steady echo at baseline must NOT trigger barge-in (energy-delta)"
    )


async def test_energy_sustained_above_echo_baseline_fires_without_stt_words():
    """Caller energy SUSTAINED clearly above the echo baseline MUST fire — even
    though the probe STT returns ZERO words (the real telephony-line case)."""
    loop = make_loop(stt=_ECHO_LINE_STT(),
                     vad=FakeVAD(speech_chunks=_BARGEIN_DELTA_SUSTAIN_FRAMES * 6))
    sent_json = []

    async def source():
        loop._playing_tts = True
        # First establish the echo baseline with steady echo (caller silent).
        for _ in range(_BARGEIN_DELTA_SUSTAIN_FRAMES + 5):
            yield _echo_steady()
        # Then the caller interrupts ON TOP of the echo — sustained above the bar.
        for _ in range(_BARGEIN_DELTA_SUSTAIN_FRAMES + 2):
            yield _caller_over_echo()
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    assert "stop_playback" in [m.get("type") for m in sent_json], (
        "Sustained energy above the echo baseline MUST trigger barge-in even "
        "with words=0 (echo blocks STT on a real phone line)"
    )


async def test_brief_spike_above_echo_does_not_trigger_bargein():
    """A brief spike above the echo baseline (a click / transient, shorter than
    the sustain window) must NOT fire — only a SUSTAINED interruption does."""
    loop = make_loop(stt=_ECHO_LINE_STT(),
                     vad=FakeVAD(speech_chunks=_BARGEIN_DELTA_SUSTAIN_FRAMES * 4))
    sent_json = []
    # A spike strictly shorter than the sustain window. With the per-quiet-frame
    # decay, a short burst can never reach the sustain threshold.
    _spike_len = max(1, _BARGEIN_DELTA_SUSTAIN_FRAMES // 2)

    async def source():
        loop._playing_tts = True
        for _ in range(_BARGEIN_DELTA_SUSTAIN_FRAMES + 5):
            yield _echo_steady()
        for _ in range(_spike_len):  # brief spike — below sustain window
            yield _caller_over_echo()
        for _ in range(_BARGEIN_DELTA_SUSTAIN_FRAMES + 5):  # back to echo
            yield _echo_steady()
        loop._playing_tts = False
        for _ in range(20):
            yield _silence()

    await loop.run(source(), lambda a: None, lambda m: sent_json.append(m))

    assert "stop_playback" not in [m.get("type") for m in sent_json], (
        f"A brief {_spike_len}-frame spike must NOT trigger barge-in (sustain "
        f"window is {_BARGEIN_DELTA_SUSTAIN_FRAMES})"
    )
