from __future__ import annotations

from voice_agent.recorder import FakeTurnStore
from voice_agent.actions import FakePublisher
from tests.conftest import make_loop, run_loop
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeTTS, FakeFreeSwitchWS


async def test_50_turn_replay():
    """Feed 50-turn fixture → assert all turns recorded, final outcome correct."""
    store = FakeTurnStore()
    pub = FakePublisher()

    # Each utterance = 10 speech + 40 silence chunks
    ws = FakeFreeSwitchWS(n_speech_chunks=10, n_silence_chunks=40)
    single_utterance = ws.all_chunks()

    # 50 utterances — last one triggers end_call
    turn_count = 0

    class CountingLLM:
        call_count = 0
        async def generate(self, ctx, user_turn, history):
            CountingLLM.call_count += 1
            action = "end_call" if CountingLLM.call_count >= 50 else "qualify"
            return FakeLLM(next_action=action).__class__(next_action=action)

    # Simpler: use FakeVAD that always cycles speech→silence
    # and FakeLLM that returns end_call on 50th turn
    class TurnTrackingLLM:
        calls = 0
        async def generate(self, ctx, user_turn, history):
            TurnTrackingLLM.calls += 1
            action = "end_call" if TurnTrackingLLM.calls >= 50 else "qualify"
            from voice_agent.models import BrainOutput
            return BrainOutput(
                reply=f"Reply {TurnTrackingLLM.calls}",
                lead_status="warm", lead_score=60,
                next_action=action, risk_level="safe",
                confidence=0.85, summary=f"Turn {TurnTrackingLLM.calls}",
            )

    stt = FakeSTT(confidence=0.90)
    tts = FakeTTS()
    guardrail_cls = __import__("tests.fakes.fake_services", fromlist=["FakeGuardrail"]).FakeGuardrail

    # Use content-based VAD so speech bytes (non-zero) and silence bytes (zero)
    # are classified correctly regardless of how often reset() is called.
    # FakeVAD is count-based and breaks with the full-duplex loop (reset after
    # each utterance causes silence chunks to be mis-classified as speech).
    from voice_agent.vad import EnergyVAD
    vad = EnergyVAD()
    llm = TurnTrackingLLM()

    loop = __import__("voice_agent.agent", fromlist=["AgentLoop"]).AgentLoop(
        ctx=__import__("tests.conftest", fromlist=["make_ctx"]).make_ctx(),
        stt=stt,
        llm=llm,
        guardrail=guardrail_cls(),
        tts=tts,
        publisher=pub,
        store=store,
        vad=vad,
    )

    # Feed exactly 55 utterances. The full-duplex loop may process all of them
    # (tasks are queued concurrently; end_call detection is polled, not immediate).
    # The important invariants are: end_call was reached (calls >= 50), all
    # processed turns are stored, and the session completed exactly once.
    all_chunks = single_utterance * 55
    await run_loop(loop, all_chunks)

    assert TurnTrackingLLM.calls >= 50, (
        f"end_call should have fired by turn 50, got {TurnTrackingLLM.calls}"
    )
    assert TurnTrackingLLM.calls <= 55, (
        f"loop processed more turns than input utterances: {TurnTrackingLLM.calls}"
    )
    assert len(store.turns) == TurnTrackingLLM.calls, (
        f"every LLM call must produce a stored turn"
    )
    # end_call fires on turn 50 and remains true for any subsequent turns
    assert store.turns[49].next_action == "end_call", (
        "turn 50 (index 49) must be end_call"
    )
    assert len(store.completed) == 1
