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
    assert llm.call_count == 1
    assert tts.call_count == 1
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
