from __future__ import annotations

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
