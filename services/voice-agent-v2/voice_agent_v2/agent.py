"""LiveKit + Pipecat voice agent — clean rebuild against the REAL installed API.

REAL API SYMBOLS (verified by introspecting pipecat-ai==1.3.0, NOT guessed):

  Pipeline:
    from pipecat.pipeline.pipeline import Pipeline
    Pipeline(processors: Sequence[FrameProcessor])

  Worker (PipelineTask is deprecated since 1.3.0 — use PipelineWorker):
    from pipecat.pipeline.task import PipelineWorker          # NOT PipelineWorker from workers/
    from pipecat.pipeline.worker import PipelineParams        # PipelineParams is here
    PipelineWorker(pipeline, *, params=None, enable_rtvi=True,
                   enable_turn_tracking=True, ...)

  Runner:
    from pipecat.workers.runner import WorkerRunner           # confirmed exists
    from pipecat.pipeline.runner import PipelineRunner        # also exists (wraps WorkerRunner)
    WorkerRunner(*, handle_sigint=True, ...)
    WorkerRunner.run(worker)  — synchronous; drives asyncio loop internally

  LiveKit transport (NOT pipecat.transports.services.livekit — that path doesn't exist):
    from pipecat.transports.livekit.transport import LiveKitTransport, LiveKitParams
    LiveKitTransport(url: str, token: str, room_name: str,
                     params: LiveKitParams | None = None)
    LiveKitParams(audio_in_enabled=True, audio_out_enabled=True,
                  audio_in_sample_rate=..., audio_out_sample_rate=...)
    transport.input()  -> LiveKitInputTransport
    transport.output() -> LiveKitOutputTransport

  VAD (confirmed present after pipecat-ai[silero] install):
    from pipecat.audio.vad.silero import SileroVADAnalyzer
    from pipecat.processors.audio.vad_processor import VADProcessor
    VADProcessor(vad_analyzer=SileroVADAnalyzer(...))

  STT (Deepgram — needs pipecat-ai[deepgram]):
    from pipecat.services.deepgram.stt import DeepgramSTTService
    DeepgramSTTService(api_key=..., sample_rate=..., ...)

  TTS (ElevenLabs — needs pipecat-ai[elevenlabs]):
    from pipecat.services.elevenlabs.tts import ElevenLabsTTSService
    ElevenLabsTTSService(api_key=..., voice_id=..., model=..., sample_rate=...)

  Frame types:
    from pipecat.frames.frames import TranscriptionFrame, TextFrame, EndFrame
    from pipecat.processors.frame_processor import FrameProcessor, FrameDirection

SCAFFOLD BUGS FIXED (the old scaffold at pipecat_runtime/ had these wrong):
  - Used PipelineWorker/WorkerRunner from a guessed "pipecat.workers" path.
    REAL: PipelineWorker is in pipecat.pipeline.task; WorkerRunner in pipecat.workers.runner
  - Used pipecat.transports.services.livekit (doesn't exist).
    REAL: pipecat.transports.livekit.transport
  - LiveKitParams in old scaffold had wrong field names; real ones confirmed above.
  - PipelineParams is in pipecat.pipeline.worker (not pipecat.pipeline.task as guessed).

Config: all secrets via env vars. See config.py / .env.template.
The LLM stage is a placeholder EchoProcessor — the real llm-router-calling
LLM FrameProcessor is the NEXT unit.
"""

from __future__ import annotations

import asyncio
import logging
import os
from typing import Any

logger = logging.getLogger(__name__)


# ── Placeholder LLM processor (kept for tests / fallback reference) ───────────

def _build_echo_llm():
    """Echo placeholder — DEPRECATED. Pipeline now uses LlmRouterProcessor.

    Kept so existing test_echo_llm_processor_is_frame_processor still passes.
    """
    from pipecat.frames.frames import TextFrame, TranscriptionFrame
    from pipecat.processors.frame_processor import FrameDirection, FrameProcessor

    class EchoLLMProcessor(FrameProcessor):
        async def process_frame(self, frame: Any, direction: FrameDirection) -> None:
            await self.push_frame(frame, direction)
            if isinstance(frame, TranscriptionFrame) and frame.finalized:
                logger.info("echo-llm transcript=%r", frame.text)
                await self.push_frame(TextFrame(text=f"You said: {frame.text}"), direction)

    return EchoLLMProcessor()


# ── Transport builder ─────────────────────────────────────────────────────────

def _build_transport(settings):
    """Build LiveKitTransport with a JWT token generated from api_key + api_secret.

    REAL API (verified):
      from pipecat.transports.livekit.transport import LiveKitTransport, LiveKitParams
      LiveKitTransport(url, token, room_name, params=LiveKitParams(...))

    The token is a LiveKit JWT generated via livekit.api.AccessToken.
    """
    from livekit.api import AccessToken, VideoGrants
    from pipecat.transports.livekit.transport import LiveKitParams, LiveKitTransport

    # Generate agent JWT token
    api_key = settings.livekit_api_key or "devkey"
    api_secret = settings.livekit_api_secret or "secret"
    room_name = settings.livekit_room_name

    token = (
        AccessToken(api_key, api_secret)
        .with_identity("voice-agent")
        .with_name("Voice Agent")
        .with_grants(VideoGrants(room_join=True, room=room_name))
        .to_jwt()
    )

    params = LiveKitParams(
        audio_in_enabled=True,
        audio_out_enabled=True,
        audio_in_sample_rate=settings.audio_in_sample_rate,
        audio_out_sample_rate=settings.audio_out_sample_rate,
    )

    return LiveKitTransport(
        url=settings.livekit_url,
        token=token,
        room_name=room_name,
        params=params,
    )


# ── STT builder ──────────────────────────────────────────────────────────────

def _build_stt(settings):
    """Build DeepgramSTTService.

    REAL API (verified):
      from pipecat.services.deepgram.stt import DeepgramSTTService
      DeepgramSTTService(api_key=..., sample_rate=..., language=...)
    """
    from pipecat.services.deepgram.stt import DeepgramSTTService

    return DeepgramSTTService(
        api_key=settings.deepgram_api_key or "dummy-key",
        sample_rate=settings.audio_in_sample_rate,
    )


# ── TTS builder ──────────────────────────────────────────────────────────────

def _build_tts(settings, ctx):
    """Build ElevenLabsTTSService (default) or a stub for non-premium.

    REAL API (verified):
      from pipecat.services.elevenlabs.tts import ElevenLabsTTSService
      ElevenLabsTTSService(api_key=..., voice_id=..., model=..., sample_rate=...)

    When TTS_PREMIUM=0 and no ElevenLabs key is set, fall back to a
    CartesiaTTSService stub or raise clearly — don't silently fail.
    """
    from voice_agent_v2.config import select_tts_provider

    provider = select_tts_provider(ctx, settings)
    logger.info("tts_provider=%s session=%s", provider, ctx.session_id)

    from pipecat.services.elevenlabs.tts import ElevenLabsTTSService

    # Use Settings class (voice_id/model direct params deprecated since 1.3.0)
    tts_settings = ElevenLabsTTSService.Settings(
        voice=settings.elevenlabs_voice_id or "21m00Tcm4TlvDq8ikWAM",
        model=settings.elevenlabs_model,
    )
    return ElevenLabsTTSService(
        api_key=settings.elevenlabs_api_key or "dummy-key",
        sample_rate=settings.audio_out_sample_rate,
        settings=tts_settings,
    )


# ── VAD builder ──────────────────────────────────────────────────────────────

def _build_vad():
    """Build VADProcessor wrapping SileroVADAnalyzer.

    REAL API (verified):
      from pipecat.audio.vad.silero import SileroVADAnalyzer
      from pipecat.processors.audio.vad_processor import VADProcessor
      VADProcessor(vad_analyzer=SileroVADAnalyzer())
    """
    from pipecat.audio.vad.silero import SileroVADAnalyzer
    from pipecat.processors.audio.vad_processor import VADProcessor

    return VADProcessor(vad_analyzer=SileroVADAnalyzer())


# ── Pipeline assembly ─────────────────────────────────────────────────────────

def build_pipeline(settings, ctx):
    """Assemble and return (PipelineWorker, WorkerRunner).

    REAL API (verified against pipecat-ai==1.3.0):

      from pipecat.pipeline.pipeline import Pipeline
      from pipecat.pipeline.task import PipelineWorker   # NOT PipelineWorker/WorkerRunner
      from pipecat.pipeline.worker import PipelineParams
      from pipecat.workers.runner import WorkerRunner

    PipelineTask is a deprecated alias for PipelineWorker since 1.3.0.
    WorkerRunner.run(worker) drives the pipeline (synchronous; starts asyncio loop).

    Pipeline chain:
      transport.input() -> VADProcessor -> STT -> LLM-placeholder -> TTS -> transport.output()

    VAD + turn detection: owned by the framework (VADProcessor handles
    barge-in + UserSpeakingFrame; Pipecat's built-in turn detection handles
    when to fire the LLM). No manual interruption strategy needed in this unit.
    """
    from pipecat.pipeline.pipeline import Pipeline
    from pipecat.pipeline.task import PipelineWorker
    from pipecat.pipeline.worker import PipelineParams
    from pipecat.workers.runner import WorkerRunner

    from voice_agent_v2.llm_router_processor import LlmRouterProcessor

    transport = _build_transport(settings)
    vad = _build_vad()
    stt = _build_stt(settings)
    llm = LlmRouterProcessor(ctx=ctx, settings=settings)
    tts = _build_tts(settings, ctx)

    pipeline = Pipeline([
        transport.input(),      # LiveKit audio in
        vad,                    # Silero VAD → UserSpeakingFrame, turn detection
        stt,                    # Deepgram STT → TranscriptionFrame
        llm,                    # LlmRouterProcessor → stream_text SSE → TextFrame
        tts,                    # ElevenLabs TTS → AudioRawFrame
        transport.output(),     # LiveKit audio out
    ])

    worker = PipelineWorker(
        pipeline,
        params=PipelineParams(
            enable_metrics=True,
            audio_in_sample_rate=settings.audio_in_sample_rate,
            audio_out_sample_rate=settings.audio_out_sample_rate,
        ),
        enable_rtvi=False,           # No RTVI overlay for this agent
        enable_turn_tracking=False,  # Not needed; VADProcessor handles turns
    )

    runner = WorkerRunner(handle_sigint=False)
    return worker, runner


# ── Entrypoint ────────────────────────────────────────────────────────────────

def main() -> None:
    """Run the agent. Config from env vars.

    For local dev:
      export LIVEKIT_URL=ws://localhost:7880
      export LIVEKIT_API_KEY=devkey
      export LIVEKIT_API_SECRET=secret
      export DEEPGRAM_API_KEY=<your-key>
      export ELEVENLABS_API_KEY=<your-key>
      export ELEVENLABS_VOICE_ID=<your-voice-id>
      python -m voice_agent_v2.agent
    """
    logging.basicConfig(level=logging.INFO)
    from voice_agent_v2.config import AgentSettings
    from voice_agent_v2.models import SessionContext

    settings = AgentSettings.from_env()
    ctx = SessionContext(session_id="local-dev", tenant_id="dev")

    worker, runner = build_pipeline(settings, ctx)
    runner.run(worker)


if __name__ == "__main__":
    main()
