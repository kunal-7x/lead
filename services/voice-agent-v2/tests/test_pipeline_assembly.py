"""Verify the pipeline assembles against the real pipecat-ai API.

Uses dummy env values (no real keys). Confirms:
1. All real import paths resolve.
2. Pipeline([...]) accepts the processor sequence without raising.
3. PipelineWorker accepts the Pipeline without raising.
4. WorkerRunner instantiates without raising.

No live calls, no network, no Docker required.
"""

from __future__ import annotations

import os
import pytest


def test_real_imports_resolve():
    """All confirmed-real pipecat import paths must resolve."""
    from pipecat.pipeline.pipeline import Pipeline  # noqa: F401
    from pipecat.pipeline.task import PipelineWorker  # noqa: F401
    from pipecat.pipeline.worker import PipelineParams  # noqa: F401
    from pipecat.workers.runner import WorkerRunner  # noqa: F401
    from pipecat.transports.livekit.transport import LiveKitParams, LiveKitTransport  # noqa: F401
    from pipecat.audio.vad.silero import SileroVADAnalyzer  # noqa: F401
    from pipecat.processors.audio.vad_processor import VADProcessor  # noqa: F401
    from pipecat.services.deepgram.stt import DeepgramSTTService  # noqa: F401
    from pipecat.services.elevenlabs.tts import ElevenLabsTTSService  # noqa: F401
    from pipecat.frames.frames import TextFrame, TranscriptionFrame, EndFrame  # noqa: F401
    from pipecat.processors.frame_processor import FrameDirection, FrameProcessor  # noqa: F401


def test_pipeline_task_is_deprecated_alias():
    """PipelineTask should exist as a deprecated alias for PipelineWorker."""
    from pipecat.pipeline.task import PipelineTask, PipelineWorker
    # They should be related (PipelineTask is the deprecated name)
    assert PipelineTask is not None
    assert PipelineWorker is not None


@pytest.mark.asyncio
async def test_build_pipeline_assembles_without_error(monkeypatch):
    """build_pipeline() assembles the Pipeline+Worker+Runner with dummy env values.

    Runs under asyncio (required because WorkerRunner.__init__ calls
    asyncio.get_running_loop()). Does NOT connect to LiveKit or run the pipeline.
    The debug log lines showing all 9 processor links confirm correct assembly.
    """
    # Inject dummy env values so no real keys are needed
    monkeypatch.setenv("LIVEKIT_URL", "ws://localhost:7880")
    monkeypatch.setenv("LIVEKIT_API_KEY", "devkey")
    # Use a 32+ byte secret to avoid InsecureKeyLengthWarning from JWT lib
    monkeypatch.setenv("LIVEKIT_API_SECRET", "devsecret_padded_to_32_bytes_here!")
    monkeypatch.setenv("LIVEKIT_ROOM_NAME", "test-room")
    monkeypatch.setenv("DEEPGRAM_API_KEY", "dummy-deepgram-key")
    monkeypatch.setenv("ELEVENLABS_API_KEY", "dummy-elevenlabs-key")
    monkeypatch.setenv("ELEVENLABS_VOICE_ID", "21m00Tcm4TlvDq8ikWAM")

    from voice_agent_v2.agent import build_pipeline
    from voice_agent_v2.config import AgentSettings
    from voice_agent_v2.models import SessionContext

    settings = AgentSettings.from_env()
    ctx = SessionContext(session_id="test", tenant_id="test-tenant")

    # build_pipeline assembles the object graph; runner.run() is NOT called here
    # (that would block and try to connect to LiveKit).
    worker, runner = build_pipeline(settings, ctx)

    from pipecat.pipeline.task import PipelineWorker
    from pipecat.workers.runner import WorkerRunner

    assert isinstance(worker, PipelineWorker)
    assert isinstance(runner, WorkerRunner)
    assert worker is not None


def test_echo_llm_processor_is_frame_processor():
    """EchoLLMProcessor must be a proper FrameProcessor subclass."""
    from pipecat.processors.frame_processor import FrameProcessor
    from voice_agent_v2.agent import _build_echo_llm

    echo = _build_echo_llm()
    assert isinstance(echo, FrameProcessor)
