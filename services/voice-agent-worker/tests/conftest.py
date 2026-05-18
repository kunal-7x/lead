from __future__ import annotations

import pytest

from voice_agent.models import SessionContext
from voice_agent.actions import FakePublisher
from voice_agent.recorder import FakeTurnStore
from voice_agent.vad import FakeVAD
from voice_agent.agent import AgentLoop
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeGuardrail, FakeTTS


def make_ctx(**kwargs) -> SessionContext:
    defaults = dict(
        session_id="sess-001",
        tenant_id="tenant-1",
        campaign_id="camp-1",
        lang="hi-en",
        voice_profile_id="meera",
        project_id="proj-1",
    )
    defaults.update(kwargs)
    return SessionContext(**defaults)


def make_loop(ctx=None, stt=None, llm=None, guardrail=None, tts=None,
              publisher=None, store=None, vad=None, **kwargs) -> AgentLoop:
    return AgentLoop(
        ctx=ctx or make_ctx(),
        stt=stt or FakeSTT(),
        llm=llm or FakeLLM(),
        guardrail=guardrail or FakeGuardrail(),
        tts=tts or FakeTTS(),
        publisher=publisher or FakePublisher(),
        store=store or FakeTurnStore(),
        vad=vad or FakeVAD(speech_chunks=10),
        **kwargs,
    )


async def run_loop(loop: AgentLoop, chunks: list[bytes]) -> dict:
    """Run agent loop over a fixed list of audio chunks, collecting output."""
    sent_audio = []
    sent_json = []

    async def audio_source():
        for chunk in chunks:
            yield chunk

    async def send_audio(audio):
        sent_audio.append(audio)

    async def send_json(msg):
        sent_json.append(msg)

    brain = await loop.run(audio_source(), send_audio, send_json)
    return {"brain": brain, "audio": sent_audio, "json": sent_json}
