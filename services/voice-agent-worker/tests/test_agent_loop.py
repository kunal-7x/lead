from __future__ import annotations

import asyncio
import inspect
from unittest.mock import patch

from tests.conftest import make_loop, make_ctx, run_loop
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeTTS, FakeFreeSwitchWS


async def test_single_turn():
    """Fake audio → fake STT → fake LLM → fake TTS → audio response sent."""
    stt = FakeSTT(transcript="price kya hai", confidence=0.90)
    llm = FakeLLM(reply="60 lakh hai.", next_action="qualify")
    tts = FakeTTS()
    store_ref = []

    from voice_agent.recorder import FakeTurnStore
    store = FakeTurnStore()
    loop = make_loop(stt=stt, llm=llm, tts=tts, store=store)

    ws = FakeFreeSwitchWS(n_speech_chunks=10, n_silence_chunks=40)
    result = await run_loop(loop, ws.all_chunks())

    assert stt.call_count == 1
    assert llm.call_count == 1  # generate() called once for parallel metadata
    # TTS call_count >= 1: filler pre-synthesis (up to 4) + reply sentences
    assert tts.call_count >= 1
    assert len(result["audio"]) >= 1
    assert result["brain"].reply == "60 lakh hai."


async def test_low_confidence_skipped():
    """STT confidence < 0.3 → LLM not called."""
    stt = FakeSTT(confidence=0.1)
    llm = FakeLLM()

    ws = FakeFreeSwitchWS()
    loop = make_loop(stt=stt, llm=llm)
    result = await run_loop(loop, ws.all_chunks())

    assert llm.call_count == 0


async def test_end_call_exits_loop():
    """next_action=end_call → loop exits after one turn."""
    llm = FakeLLM(next_action="end_call")
    ws = FakeFreeSwitchWS()
    loop = make_loop(llm=llm)
    result = await run_loop(loop, ws.all_chunks())
    assert result["brain"].next_action == "end_call"


async def test_dialog_history_grows():
    """Dialog history grows with each turn (VAD chunks sized for 2 utterances)."""
    from voice_agent.vad import FakeVAD

    # FakeVAD: first 10 = speech, next 40 = silence, repeat → 2 utterances
    vad = FakeVAD(speech_chunks=10)
    llm = FakeLLM()
    stt = FakeSTT(confidence=0.90)

    ws = FakeFreeSwitchWS()
    chunks = ws.all_chunks() + ws.all_chunks()  # 2 utterances
    loop = make_loop(stt=stt, llm=llm, vad=vad)
    await run_loop(loop, chunks)

    assert llm.call_count == 2
    assert len(loop._dialog_history) == 4  # 2 user + 2 assistant


async def test_greeting_played_first():
    """If greeting_audio is set, it is played before the loop."""
    greeting = b"\x10\x20" * 100
    loop = make_loop(greeting_audio=greeting)

    ws = FakeFreeSwitchWS()
    result = await run_loop(loop, ws.all_chunks())

    assert result["audio"][0] == greeting


async def test_call_complete_json_sent():
    """call_complete JSON message always sent at end."""
    ws = FakeFreeSwitchWS()
    loop = make_loop()
    result = await run_loop(loop, ws.all_chunks())
    types = [m.get("type") for m in result["json"]]
    assert "call_complete" in types


async def test_collected_slots_accumulated_and_passed():
    """Workstream D: after turn 1 brain sets budget+location, turn 2 gets those slots."""
    from voice_agent.models import BrainOutput
    from voice_agent.vad import FakeVAD

    # Build a FakeLLM that returns budget+location on first turn
    class SlottedFakeLLM(FakeLLM):
        def __init__(self):
            super().__init__()
            self.slots_received: list = []

        def _make_brain(self):
            # Return a brain with budget text and location set
            base = super()._make_brain()
            return base.model_copy(update={
                "budget": {"value": 7500000, "text": "75 लाख", "confidence": 0.9},
                "location_pref": "सरोजनी नगर",
            })

        async def generate(self, ctx, user_turn, dialog_history,
                           collected_slots=None):
            self.slots_received.append(collected_slots)
            self.call_count += 1
            self.last_collected_slots = collected_slots
            return self._make_brain()

        async def generate_stream_text(self, ctx, user_turn, dialog_history,
                                        collected_slots=None):
            brain = self._make_brain()
            words = brain.reply.split()
            for i, word in enumerate(words):
                token = word + (" " if i < len(words) - 1 else "")
                yield (token, None)

    llm = SlottedFakeLLM()
    stt = FakeSTT(confidence=0.90)
    vad = FakeVAD(speech_chunks=10)
    ws = FakeFreeSwitchWS()
    chunks = ws.all_chunks() + ws.all_chunks()  # 2 utterances
    loop = make_loop(stt=stt, llm=llm, vad=vad)
    await run_loop(loop, chunks)

    # After both turns, accumulated_slots must contain budget + location from FakeLLM brain.
    # (Slots accumulate in _accumulated_slots across the call regardless of turn ordering.)
    assert llm.call_count == 2
    slots = loop._accumulated_slots
    assert slots.get("budget_text") == "75 लाख", (
        f"budget_text not accumulated; got: {slots}"
    )
    assert slots.get("location_pref") == "सरोजनी नगर", (
        f"location_pref not accumulated; got: {slots}"
    )
    assert slots.get("budget_value") == 7500000, (
        f"budget_value not accumulated; got: {slots}"
    )


# ── Workstream H: gap-triggered filler tests ─────────────────────────────────

async def test_filler_played_when_tts_delayed():
    """Filler plays when TTS first audio is delayed beyond the gap threshold."""
    import voice_agent.agent as agent_mod

    TINY_GAP_MS = 30  # very short gap so test doesn't sleep long

    # Slow TTS: sleeps longer than the gap before returning audio
    class SlowTTS(FakeTTS):
        async def synthesize(self, text, lang, voice_id, tenant_id, session_id,
                             tts_premium=False):
            await asyncio.sleep(TINY_GAP_MS / 1000 * 3)  # 3× the gap → filler fires
            return await super().synthesize(text, lang, voice_id, tenant_id,
                                            session_id, tts_premium)

    stt = FakeSTT(confidence=0.90)
    llm = FakeLLM()
    tts = SlowTTS()

    filler_played_texts: list[str] = []
    original_play = agent_mod.AgentLoop._play_filler

    async def spy_play_filler(self, send_audio, first_audio_event, t_speech_end=None):
        # Pre-populate cache with a known audio blob so filler can "play"
        for t in agent_mod._FILLER_TEXTS:
            if t not in agent_mod._filler_cache:
                agent_mod._filler_cache[t] = b"\x00\x00" * 100

        # Capture which text would be played by wrapping send_audio
        original_send = send_audio
        played = []

        async def capturing_send(audio):
            played.append(audio)
            await original_send(audio) if inspect.iscoroutinefunction(original_send) else original_send(audio)

        await original_play(self, capturing_send, first_audio_event, t_speech_end)
        if played:
            filler_played_texts.append("played")

    with patch.object(agent_mod.AgentLoop, "_play_filler", spy_play_filler), \
         patch.object(agent_mod, "_FILLER_ENABLED", True), \
         patch.object(agent_mod, "_FILLER_GAP_MS", float(TINY_GAP_MS)), \
         patch.object(agent_mod, "_FILLER_DELAY_MS", float(TINY_GAP_MS)):
        ws = FakeFreeSwitchWS()
        loop = make_loop(stt=stt, llm=llm, tts=tts)
        await run_loop(loop, ws.all_chunks())

    # Filler must have been triggered at least once (slow TTS → gap fires)
    assert len(filler_played_texts) >= 1, (
        "Expected filler to play on slow TTS turn but it was skipped"
    )
    # At most once per turn (single utterance → single filler max)
    assert len(filler_played_texts) <= 1, (
        f"Filler played {len(filler_played_texts)} times on a single turn — must be at-most-once"
    )


async def test_filler_skipped_when_tts_fast():
    """Filler is NOT played when TTS first audio arrives before the gap threshold."""
    import voice_agent.agent as agent_mod

    GENEROUS_GAP_MS = 5000  # very large gap — fast TTS will always beat it

    stt = FakeSTT(confidence=0.90)
    llm = FakeLLM()
    tts = FakeTTS()  # instant TTS — no delay

    filler_played_texts: list[str] = []
    original_play = agent_mod.AgentLoop._play_filler

    async def spy_play_filler(self, send_audio, first_audio_event, t_speech_end=None):
        # Pre-populate cache
        for t in agent_mod._FILLER_TEXTS:
            if t not in agent_mod._filler_cache:
                agent_mod._filler_cache[t] = b"\x00\x00" * 100

        original_send = send_audio
        played = []

        async def capturing_send(audio):
            played.append(audio)
            await original_send(audio) if inspect.iscoroutinefunction(original_send) else original_send(audio)

        await original_play(self, capturing_send, first_audio_event, t_speech_end)
        if played:
            filler_played_texts.append("played")

    with patch.object(agent_mod.AgentLoop, "_play_filler", spy_play_filler), \
         patch.object(agent_mod, "_FILLER_ENABLED", True), \
         patch.object(agent_mod, "_FILLER_GAP_MS", float(GENEROUS_GAP_MS)), \
         patch.object(agent_mod, "_FILLER_DELAY_MS", float(GENEROUS_GAP_MS)):
        ws = FakeFreeSwitchWS()
        loop = make_loop(stt=stt, llm=llm, tts=tts)
        await run_loop(loop, ws.all_chunks())

    # Fast TTS should have set first_audio_event before the gap fired → no filler
    assert len(filler_played_texts) == 0, (
        f"Filler played on a fast-TTS turn — should have been skipped; played={filler_played_texts}"
    )


# ── Bug-fix regression tests (call 237210af) ─────────────────────────────────

async def test_filler_not_fired_on_greeting_noise_or_pre_utterance():
    """Bug A: filler must NOT fire on noise blips before the first real user turn.

    Simulates the call 237210af scenario: STT is slow (>gap) AND returns empty/
    noise transcript (noise-gate rejection). Filler must stay silent even though
    the gap timer fires — reason=pre_utterance_lockout must appear in [diag].

    A slow-STT + noise-transcript utterance is the exact double-fire scenario:
    old code would have played filler (gap fired before noise gate cancelled);
    new code must suppress it via pre_utterance_lockout.
    """
    import voice_agent.agent as agent_mod

    TINY_GAP_MS = 30

    class SlowSTT(FakeSTT):
        """Returns empty transcript but only after the filler gap has elapsed."""
        async def transcribe(self, audio, lang, session_id):
            await asyncio.sleep(TINY_GAP_MS / 1000 * 4)  # 4× gap → filler would fire
            return await super().transcribe(audio, lang, session_id)

    # STT returns empty → noise gate rejects → _real_utterance_seen never set
    noise_stt = SlowSTT(transcript="", confidence=0.9)
    llm = FakeLLM()
    tts = FakeTTS()  # TTS is irrelevant — we never reach LLM/TTS

    filler_played_texts: list[str] = []
    lockout_logged: list[str] = []
    original_play = agent_mod.AgentLoop._play_filler

    async def spy_play_filler(self, send_audio, first_audio_event, t_speech_end=None):
        for t in agent_mod._FILLER_TEXTS:
            if t not in agent_mod._filler_cache:
                agent_mod._filler_cache[t] = b"\x00\x00" * 100

        played = []
        original_send = send_audio

        async def capturing_send(audio):
            played.append(audio)
            if inspect.iscoroutinefunction(original_send):
                await original_send(audio)
            else:
                original_send(audio)

        # Capture lockout diag
        original_diag = agent_mod._diag

        def spy_diag(session, turn, **kw):
            if kw.get("reason") == "pre_utterance_lockout":
                lockout_logged.append("lockout")
            original_diag(session, turn, **kw)

        with patch.object(agent_mod, "_diag", spy_diag):
            await original_play(self, capturing_send, first_audio_event, t_speech_end)
        if played:
            filler_played_texts.append("played")

    with patch.object(agent_mod.AgentLoop, "_play_filler", spy_play_filler), \
         patch.object(agent_mod, "_FILLER_ENABLED", True), \
         patch.object(agent_mod, "_FILLER_GAP_MS", float(TINY_GAP_MS)), \
         patch.object(agent_mod, "_FILLER_DELAY_MS", float(TINY_GAP_MS)):
        ws = FakeFreeSwitchWS(n_speech_chunks=10, n_silence_chunks=40)
        loop = make_loop(stt=noise_stt, llm=llm, tts=tts)
        await run_loop(loop, ws.all_chunks())

    assert len(filler_played_texts) == 0, (
        f"Filler played on pre-utterance noise blip — must be locked out; "
        f"played={filler_played_texts}"
    )
    assert len(lockout_logged) >= 1, (
        "Expected [diag] reason=pre_utterance_lockout but none was emitted"
    )


async def test_filler_fires_on_each_slow_turn_per_turn_not_per_call():
    """Bug B: filler must fire on EACH slow turn (per-turn guard, not per-call).

    Runs two consecutive utterances with slow TTS. Both must trigger filler
    independently — proves the at-most-once guard resets per turn, not per call.
    """
    import voice_agent.agent as agent_mod
    from voice_agent.vad import FakeVAD

    TINY_GAP_MS = 30

    class SlowTTS(FakeTTS):
        async def synthesize(self, text, lang, voice_id, tenant_id, session_id,
                             tts_premium=False):
            await asyncio.sleep(TINY_GAP_MS / 1000 * 3)
            return await super().synthesize(text, lang, voice_id, tenant_id,
                                            session_id, tts_premium)

    stt = FakeSTT(transcript="kya price hai", confidence=0.90)
    llm = FakeLLM()
    tts = SlowTTS()
    vad = FakeVAD(speech_chunks=10)

    filler_fired_turns: list[int] = []
    original_play = agent_mod.AgentLoop._play_filler

    async def spy_play_filler(self, send_audio, first_audio_event, t_speech_end=None):
        for t in agent_mod._FILLER_TEXTS:
            if t not in agent_mod._filler_cache:
                agent_mod._filler_cache[t] = b"\x00\x00" * 100

        played = []
        original_send = send_audio

        async def capturing_send(audio):
            played.append(audio)
            if inspect.iscoroutinefunction(original_send):
                await original_send(audio)
            else:
                original_send(audio)

        await original_play(self, capturing_send, first_audio_event, t_speech_end)
        if played:
            filler_fired_turns.append(self._turn_index)

    with patch.object(agent_mod.AgentLoop, "_play_filler", spy_play_filler), \
         patch.object(agent_mod, "_FILLER_ENABLED", True), \
         patch.object(agent_mod, "_FILLER_GAP_MS", float(TINY_GAP_MS)), \
         patch.object(agent_mod, "_FILLER_DELAY_MS", float(TINY_GAP_MS)):
        ws = FakeFreeSwitchWS(n_speech_chunks=10, n_silence_chunks=40)
        chunks = ws.all_chunks() + ws.all_chunks()  # 2 real utterances
        loop = make_loop(stt=stt, llm=llm, tts=tts, vad=vad)
        await run_loop(loop, chunks)

    assert len(filler_fired_turns) >= 2, (
        f"Expected filler on both slow turns but only fired on turns: {filler_fired_turns}. "
        "At-most-once guard must be per-turn, not per-call."
    )


async def test_filler_at_most_once_within_single_turn():
    """Bug B: at-most-once within a single turn — filler fires at most once per turn."""
    import voice_agent.agent as agent_mod

    TINY_GAP_MS = 30

    class SlowTTS(FakeTTS):
        async def synthesize(self, text, lang, voice_id, tenant_id, session_id,
                             tts_premium=False):
            await asyncio.sleep(TINY_GAP_MS / 1000 * 3)
            return await super().synthesize(text, lang, voice_id, tenant_id,
                                            session_id, tts_premium)

    stt = FakeSTT(transcript="kya price hai", confidence=0.90)
    llm = FakeLLM()
    tts = SlowTTS()

    filler_play_count: list[int] = []
    original_play = agent_mod.AgentLoop._play_filler

    async def spy_play_filler(self, send_audio, first_audio_event, t_speech_end=None):
        for t in agent_mod._FILLER_TEXTS:
            if t not in agent_mod._filler_cache:
                agent_mod._filler_cache[t] = b"\x00\x00" * 100

        played = []
        original_send = send_audio

        async def capturing_send(audio):
            played.append(audio)
            if inspect.iscoroutinefunction(original_send):
                await original_send(audio)
            else:
                original_send(audio)

        await original_play(self, capturing_send, first_audio_event, t_speech_end)
        filler_play_count.append(len(played))

    with patch.object(agent_mod.AgentLoop, "_play_filler", spy_play_filler), \
         patch.object(agent_mod, "_FILLER_ENABLED", True), \
         patch.object(agent_mod, "_FILLER_GAP_MS", float(TINY_GAP_MS)), \
         patch.object(agent_mod, "_FILLER_DELAY_MS", float(TINY_GAP_MS)):
        ws = FakeFreeSwitchWS(n_speech_chunks=10, n_silence_chunks=40)
        loop = make_loop(stt=stt, llm=llm, tts=tts)
        await run_loop(loop, ws.all_chunks())

    # _play_filler should be called once (one filler_task per _process_utterance),
    # and within that one call, at most one audio blob sent.
    total_played = sum(filler_play_count)
    assert total_played <= 1, (
        f"Filler fired {total_played} times within a single turn — must be at-most-once"
    )
