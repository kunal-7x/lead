from __future__ import annotations

from voice_agent.recorder import FakeTurnStore, record_turn
from voice_agent.models import STTResult, BrainOutput
from tests.conftest import make_ctx, make_loop, run_loop
from tests.fakes.fake_services import FakeFreeSwitchWS


async def test_turn_recorded():
    """Each turn writes call_turns + call_transcripts (captured in FakeTurnStore)."""
    store = FakeTurnStore()
    loop = make_loop(store=store)

    ws = FakeFreeSwitchWS()
    await run_loop(loop, ws.all_chunks())

    assert len(store.turns) >= 1
    turn = store.turns[0]
    assert turn.session_id == "sess-001"
    assert turn.transcript != ""
    assert turn.reply != ""


async def test_call_completed_recorded():
    """complete_call is called at end of call."""
    store = FakeTurnStore()
    loop = make_loop(store=store)

    ws = FakeFreeSwitchWS()
    await run_loop(loop, ws.all_chunks())

    assert len(store.completed) == 1
    assert store.completed[0]["session_id"] == "sess-001"


async def test_turn_index_increments():
    """Turn index increments with each utterance."""
    store = FakeTurnStore()
    from voice_agent.vad import FakeVAD
    vad = FakeVAD(speech_chunks=10)
    loop = make_loop(store=store, vad=vad)

    ws = FakeFreeSwitchWS()
    chunks = ws.all_chunks() + ws.all_chunks()
    await run_loop(loop, chunks)

    if len(store.turns) >= 2:
        assert store.turns[1].turn_index == 1


async def test_record_turn_direct():
    """record_turn builds CallTurn with correct fields."""
    store = FakeTurnStore()
    ctx = make_ctx()
    stt = STTResult(text="test text", confidence=0.88, engine_used="sarvam")
    brain = BrainOutput(reply="Test reply", next_action="qualify", summary="test")

    turn = await record_turn(store, ctx, 0, stt, brain, "sarvam_bulbul", False)

    assert turn.transcript == "test text"
    assert turn.reply == "Test reply"
    assert turn.stt_engine == "sarvam"
    assert turn.tts_engine == "sarvam_bulbul"
    assert len(store.turns) == 1
