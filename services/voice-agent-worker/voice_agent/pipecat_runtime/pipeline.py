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

    API-CHECK: pipecat.services.sarvam.stt.SarvamSTTService ctor kwargs
    (api_key, model, language/params). Verify against pipecat-ai==1.3.*.
    """
    from pipecat.services.sarvam.stt import SarvamSTTService  # type: ignore

    return SarvamSTTService(
        api_key=settings.sarvam_api_key,
        model=settings.sarvam_stt_model,           # saaras:v3
        language=ctx.lang or "hi-IN",              # API-CHECK: param name (language vs lang)
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
    """FastAPI WS transport with VAD + Smart-Turn v3 + 8 kHz audio in/out.

    API-CHECK: pipecat.transports.websocket.fastapi.{FastAPIWebsocketTransport,
    FastAPIWebsocketParams}; pipecat.audio.vad.silero.SileroVADAnalyzer;
    pipecat.audio.turn.smart_turn.local_smart_turn_v3.LocalSmartTurnAnalyzerV3.
    v1.0 removed vad_enabled flags — pass analyzer objects only.
    """
    from pipecat.audio.turn.smart_turn.local_smart_turn_v3 import (  # type: ignore
        LocalSmartTurnAnalyzerV3,
    )
    from pipecat.audio.vad.silero import SileroVADAnalyzer  # type: ignore
    from pipecat.transports.websocket.fastapi import (  # type: ignore
        FastAPIWebsocketParams,
        FastAPIWebsocketTransport,
    )

    params = FastAPIWebsocketParams(
        serializer=serializer,
        audio_in_enabled=True,
        audio_out_enabled=True,
        add_wav_header=False,
        vad_analyzer=SileroVADAnalyzer(),
        turn_analyzer=LocalSmartTurnAnalyzerV3(),
        session_timeout=settings.session_timeout_s,
        audio_out_sample_rate=settings.output_sample_rate,  # API-CHECK: 8k out param name
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
    """Build + return (PipelineTask, PipelineRunner) for one call.

    Imports the heavy pipecat symbols lazily. Raises if pipecat is not installed —
    the caller (app.pipecat_ws) logs and closes the socket.
    """
    # API-CHECK: pipeline/task/runner module paths + PipelineParams fields
    # (allow_interruptions, interruption_strategies, enable_metrics).
    from pipecat.pipeline.pipeline import Pipeline  # type: ignore
    from pipecat.pipeline.runner import PipelineRunner  # type: ignore
    from pipecat.pipeline.task import PipelineParams, PipelineTask  # type: ignore

    # API-CHECK: MinWordsInterruptionStrategy import location. Documented under
    # pipecat.audio.interruptions; some builds expose it via pipecat.processors.
    try:
        from pipecat.audio.interruptions.min_words_interruption_strategy import (  # type: ignore
            MinWordsInterruptionStrategy,
        )
    except Exception:  # noqa: BLE001
        from pipecat.processors.aggregators.llm_response import (  # type: ignore  # noqa: F401
            MinWordsInterruptionStrategy,
        )

    call_ctx = CallContext(session=ctx, settings=settings)

    serializer = _build_serializer(stream_id, call_id)
    transport = _build_transport(websocket, serializer, settings)
    stt = _build_stt(settings, ctx)
    tts, provider = _build_tts(settings, ctx)
    logger.info("pipecat pipeline tts_provider=%s call_id=%s", provider, call_id)

    cso = CsoProcessor(call_ctx)
    llm = LlmRouterProcessor(call_ctx)
    actions = ActionsProcessor(call_ctx, publisher, turn_store=turn_store)

    pipeline = Pipeline([
        transport.input(),
        stt,
        cso,
        llm,
        actions,     # taps BrainOutputFrame (consumed here); passes audio/text through
        tts,
        transport.output(),
    ])

    task = PipelineTask(
        pipeline,
        params=PipelineParams(
            allow_interruptions=True,
            interruption_strategies=[MinWordsInterruptionStrategy(min_words=2)],
            enable_metrics=True,
            audio_out_sample_rate=settings.output_sample_rate,  # API-CHECK: task-level 8k param
        ),
    )
    runner = PipelineRunner(handle_sigint=False)
    return task, runner
