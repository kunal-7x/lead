from __future__ import annotations

from voice_agent.vad import FakeVAD
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

    from voice_agent.vad import FakeVAD

    # Build 50 utterances worth of chunks
    # FakeVAD resets between utterances
    class ResetVAD:
        def __init__(self):
            self._inner = FakeVAD(speech_chunks=10)
        def is_speech(self, chunk):
            return self._inner.is_speech(chunk)
        def reset(self):
            self._inner = FakeVAD(speech_chunks=10)

    vad = ResetVAD()
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

    all_chunks = single_utterance * 55  # extra chunks, loop exits at turn 50
    await run_loop(loop, all_chunks)

    assert TurnTrackingLLM.calls == 50
    assert len(store.turns) == 50
    assert store.turns[-1].next_action == "end_call"
    assert len(store.completed) == 1
