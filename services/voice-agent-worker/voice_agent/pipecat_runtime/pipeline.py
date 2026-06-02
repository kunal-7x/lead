"""build_pipeline — assemble the targeted-hybrid Pipecat pipeline.

Chain:
    transport.input()
      → SarvamSTTService
      → CsoProcessor
      → LlmRouterProcessor
      → TTS (Sarvam bulbul:v3 | ElevenLabs, per ctx)
      → transport.output()
  + ActionsProcessor tapping BrainOutputFrame (placed after the LLM processor so it
    sees BrainOutputFrames but those frames are consumed there, not sent to TTS).

Transport wiring: VobizFrameSerializer on a FastAPIWebsocketTransport, with
SileroVADAnalyzer + LocalSmartTurnAnalyzerV3 + MinWordsInterruptionStrategy(min_words=2).
TTS output is forced to 8 kHz for the Vobiz serializer.

ALL pipecat imports happen INSIDE this function so the module imports cleanly on a
box without the heavy stack (the app.py WS handler imports build_pipeline lazily).
Every uncertain symbol carries an ``# API-CHECK:`` note — verify against the real
pipecat-ai==1.3.* on the droplet.
"""

from __future__ import annotations

import logging
import os

from voice_agent.models import SessionContext
from voice_agent.pipecat_runtime.actions_processor import ActionsProcessor
from voice_agent.pipecat_runtime.config import PipecatSettings, select_tts_provider
from voice_agent.pipecat_runtime.cso_processor import CallContext, CsoProcessor
from voice_agent.pipecat_runtime.llm_router_service import LlmRouterProcessor

logger = logging.getLogger(__name__)


def _build_serializer(stream_id: str, call_id: str):
    """Construct the Vobiz serializer, falling back to Plivo's (identical shapes).

    API-CHECK: pipecat.serializers.vobiz.VobizFrameSerializer ctor
    (stream_id, call_id=None, auth_id=None, auth_token=None, params=...).
    Fallback: pipecat.serializers.plivo.PlivoFrameSerializer (same playAudio/
    clearAudio/media shapes). Verify both vs the installed libs.
    """
    auth_id = os.getenv("VOBIZ_AUTH_ID") or os.getenv("PLIVO_AUTH_ID")
    auth_token = os.getenv("VOBIZ_AUTH_TOKEN") or os.getenv("PLIVO_AUTH_TOKEN")
    try:
        from pipecat.serializers.vobiz import VobizFrameSerializer  # type: ignore

        return VobizFrameSerializer(
            stream_id=stream_id,
            call_id=call_id,
            auth_id=auth_id,
            auth_token=auth_token,
        )
    except Exception as exc:  # noqa: BLE001
        logger.warning(
            "VobizFrameSerializer unavailable (%r); falling back to PlivoFrameSerializer",
            exc,
        )
        # API-CHECK: PlivoFrameSerializer ctor signature (stream_id/call_id/params).
        from pipecat.serializers.plivo import PlivoFrameSerializer  # type: ignore

        return PlivoFrameSerializer(
            stream_id=stream_id,
            call_id=call_id,
            auth_id=auth_id,
            auth_token=auth_token,
        )


def _build_stt(settings: PipecatSettings, ctx: SessionContext):
    """Native Sarvam STT (bypasses stt-router — keepalive WS).

    SarvamSTTService ctor (verified pipecat-ai==1.3.*):
      api_key, model (deprecated → use settings), mode (transcribe|translate|
      verbatim|translit|codemix), sample_rate, input_audio_codec, settings.
    No `language` param; language is embedded in `mode` or model default.
    For Hindi+English codemix use mode="codemix".
    """
    from pipecat.services.sarvam.stt import SarvamSTTService  # type: ignore

    # mode="codemix" handles Hindi+English mixed speech (hi-en).
    # sample_rate left as None → Sarvam default 16 kHz (transport resamples from 8k).
    return SarvamSTTService(
        api_key=settings.sarvam_api_key,
        model=settings.sarvam_stt_model,   # saaras:v3
        mode="codemix",                    # hi-en codemix
    )


def _build_tts(settings: PipecatSettings, ctx: SessionContext):
    """Native TTS, provider-selected per call; output resampled to 8 kHz.

    Sarvam emits 24 kHz → the transport/pipeline resamples to 8 k. ElevenLabs is
    requested at 8 k natively. Both must be switchable (founder requirement).
    """
    provider = select_tts_provider(ctx, settings)
    if provider == "elevenlabs":
        # API-CHECK: pipecat.services.elevenlabs.tts.ElevenLabsTTSService ctor
        # (api_key, voice_id, model, params/output_format with 8k sample_rate).
        from pipecat.services.elevenlabs.tts import ElevenLabsTTSService  # type: ignore

        try:
            from pipecat.transcriptions.language import Language  # type: ignore  # noqa: F401
        except Exception:  # noqa: BLE001
            pass
        return ElevenLabsTTSService(
            api_key=settings.elevenlabs_api_key,
            voice_id=settings.elevenlabs_voice_id,
            model=settings.elevenlabs_model,
            sample_rate=settings.output_sample_rate,   # API-CHECK: 8k via sample_rate vs params.output_format
        ), provider

    # API-CHECK: pipecat.services.sarvam.tts.SarvamTTSService ctor
    # (api_key, model=bulbul:v3, voice_id/speaker=ctx.voice_profile_id, sample_rate).
    from pipecat.services.sarvam.tts import SarvamTTSService  # type: ignore

    return SarvamTTSService(
        api_key=settings.sarvam_api_key,
        model=settings.sarvam_tts_model,               # bulbul:v3
        voice_id=ctx.voice_profile_id or settings.sarvam_tts_speaker,  # API-CHECK: voice_id vs speaker kwarg
        sample_rate=settings.output_sample_rate,        # request/resample to 8k for Vobiz
    ), provider


def _build_transport(websocket, serializer, settings: PipecatSettings):
    """FastAPI WS transport — pipecat 1.3 API.

    FastAPIWebsocketParams (verified 1.3.*) inherits from TransportParams:
      audio_in_enabled, audio_out_enabled, audio_in_sample_rate,
      audio_out_sample_rate (all on TransportParams).
    FastAPIWebsocketParams adds: add_wav_header, serializer, session_timeout.
    VAD/turn are NOT transport params in 1.3 — inject VADProcessor + turn
    strategy in the pipeline instead (see build_pipeline).
    """
    from pipecat.transports.websocket.fastapi import (  # type: ignore
        FastAPIWebsocketParams,
        FastAPIWebsocketTransport,
    )

    params = FastAPIWebsocketParams(
        serializer=serializer,
        audio_in_enabled=True,
        audio_out_enabled=True,
        add_wav_header=False,
        audio_in_sample_rate=8000,                         # Vobiz sends µ-law 8k
        audio_out_sample_rate=settings.output_sample_rate,  # 8k out
        session_timeout=settings.session_timeout_s,
    )
    return FastAPIWebsocketTransport(websocket=websocket, params=params)


async def build_pipeline(
    ctx: SessionContext,
    websocket,
    settings: PipecatSettings,
    publisher,
    stream_id: str,
    call_id: str,
    turn_store=None,
):
    """Build + return (PipelineWorker, WorkerRunner) for one call.

    Pipecat 1.3 API (verified on box):
    - PipelineTask/PipelineRunner are deprecated — use PipelineWorker/WorkerRunner.
    - VAD is a VADProcessor in the pipeline (NOT a transport param).
    - Turn detection is the default TurnAnalyzerUserTurnStopStrategy (SmartTurnV3).
    - allow_interruptions/interruption_strategies removed from PipelineParams.
    - PipelineParams fields: audio_out_sample_rate, enable_metrics, etc.
    - WorkerRunner.run(worker) drives the pipeline.
    """
    from pipecat.audio.vad.silero import SileroVADAnalyzer  # type: ignore
    from pipecat.pipeline.pipeline import Pipeline  # type: ignore
    from pipecat.pipeline.task import PipelineParams, PipelineWorker  # type: ignore
    from pipecat.processors.audio.vad_processor import VADProcessor  # type: ignore
    from pipecat.workers.runner import WorkerRunner  # type: ignore

    call_ctx = CallContext(session=ctx, settings=settings)

    serializer = _build_serializer(stream_id, call_id)
    transport = _build_transport(websocket, serializer, settings)
    stt = _build_stt(settings, ctx)
    tts, provider = _build_tts(settings, ctx)
    logger.info("pipecat pipeline tts_provider=%s call_id=%s", provider, call_id)

    # VADProcessor replaces the old vad_analyzer transport param (pipecat 1.3).
    # Sits between transport.input() and STT to drive barge-in + turn detection.
    vad = VADProcessor(vad_analyzer=SileroVADAnalyzer())

    cso = CsoProcessor(call_ctx)
    llm = LlmRouterProcessor(call_ctx)
    actions = ActionsProcessor(call_ctx, publisher, turn_store=turn_store)

    pipeline = Pipeline([
        transport.input(),
        vad,         # Silero VAD → drives UserSpeakingFrame / turn detection
        stt,
        cso,
        llm,
        actions,     # taps BrainOutputFrame (consumed here); passes audio/text through
        tts,
        transport.output(),
    ])

    worker = PipelineWorker(
        pipeline,
        params=PipelineParams(
            enable_metrics=True,
            audio_out_sample_rate=settings.output_sample_rate,  # 8k for Vobiz
        ),
        enable_rtvi=False,   # No RTVI overlay; also avoids RTVIProcessor pipeline walk
        enable_turn_tracking=False,  # Turn tracking not needed for Vobiz
    )
    runner = WorkerRunner(handle_sigint=False)
    return worker, runner
