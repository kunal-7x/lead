from __future__ import annotations

import asyncio
import base64
import inspect
import json
import time
from typing import AsyncIterator

from voice_agent.actions import Publisher, handle_actions
from voice_agent.clients import STTClient, LLMClient, GuardrailClient, TTSClient
from voice_agent.models import SessionContext, STTResult, BrainOutput
from voice_agent.recorder import TurnStore, record_turn
from voice_agent.vad import VAD, SILENCE_THRESHOLD_MS, CHUNK_MS

_MIN_CONFIDENCE = 0.3
_END_ACTIONS = {"end_call", "opt_out"}


async def _call(fn, *args):
    result = fn(*args)
    if inspect.isawaitable(result):
        await result


class AgentLoop:
    """Core per-call agent loop.

    Injected with all service clients and infrastructure — fully testable.
    """

    def __init__(
        self,
        ctx: SessionContext,
        stt: STTClient,
        llm: LLMClient,
        guardrail: GuardrailClient,
        tts: TTSClient,
        publisher: Publisher,
        store: TurnStore,
        vad: VAD,
        greeting_audio: bytes | None = None,
    ) -> None:
        self.ctx = ctx
        self._stt = stt
        self._llm = llm
        self._guardrail = guardrail
        self._tts = tts
        self._publisher = publisher
        self._store = store
        self._vad = vad
        self._greeting_audio = greeting_audio
        self._dialog_history: list[dict] = []
        self._turn_index = 0
        self._playing_tts = False
        self._stop_playback = asyncio.Event()

    async def run(self, audio_source: AsyncIterator[bytes],
                  send_audio: callable, send_json: callable) -> BrainOutput | None:
        """Main agent loop. Returns final brain output when call ends."""
        # Play greeting if available
        if self._greeting_audio:
            await _call(send_audio, self._greeting_audio)

        silence_chunks_needed = SILENCE_THRESHOLD_MS // CHUNK_MS
        speech_started = False
        silence_count = 0
        audio_buffer = bytearray()
        last_brain: BrainOutput | None = None

        async for chunk in audio_source:
            is_speech = self._vad.is_speech(chunk)

            # Barge-in: caller speaks while TTS playing
            if is_speech and self._playing_tts:
                self._stop_playback.set()
                await _call(send_json, {"type": "stop_playback"})
                self._playing_tts = False
                audio_buffer.clear()
                speech_started = False
                silence_count = 0

            if is_speech:
                speech_started = True
                silence_count = 0
                audio_buffer.extend(chunk)
            elif speech_started:
                silence_count += 1
                audio_buffer.extend(chunk)
                if silence_count >= silence_chunks_needed:
                    # Utterance complete — run pipeline
                    last_brain = await self._process_utterance(
                        bytes(audio_buffer), send_audio, send_json
                    )
                    audio_buffer.clear()
                    speech_started = False
                    silence_count = 0
                    self._vad.reset()

                    if last_brain and last_brain.next_action in _END_ACTIONS:
                        break

        # End of call
        await self._finalize(last_brain, send_json)
        return last_brain

    async def _process_utterance(
        self, audio: bytes, send_audio: callable, send_json: callable
    ) -> BrainOutput:
        t0 = time.time()

        # STT
        stt_result = await self._stt.transcribe(audio, self.ctx.lang, self.ctx.session_id)

        if not stt_result.text or stt_result.confidence < _MIN_CONFIDENCE:
            return BrainOutput(reply="", next_action="qualify", summary="")

        # LLM + guardrail
        brain = await self._llm.generate(self.ctx, stt_result.text, self._dialog_history)
        brain = await self._guardrail.check(brain, [], stt_result.text)

        # TTS
        self._stop_playback.clear()
        self._playing_tts = True
        tts_result = await self._tts.synthesize(
            brain.reply, self.ctx.lang, self.ctx.voice_profile_id,
            self.ctx.tenant_id, self.ctx.session_id, self.ctx.tts_premium,
        )
        if not self._stop_playback.is_set():
            await _call(send_audio, tts_result.audio)
        self._playing_tts = False

        # Record turn
        await record_turn(
            self._store, self.ctx, self._turn_index,
            stt_result, brain, tts_result.tier_used, tts_result.cache_hit,
        )

        # Actions
        await handle_actions(brain, self.ctx, self._publisher)

        # History
        self._dialog_history.append({"role": "user", "content": stt_result.text})
        self._dialog_history.append({"role": "assistant", "content": brain.reply})
        self._turn_index += 1

        return brain

    async def _finalize(self, last_brain: BrainOutput | None, send_json: callable) -> None:
        outcome = last_brain.next_action if last_brain else "unknown"
        summary = last_brain.summary if last_brain else "Call ended"
        await self._store.complete_call(self.ctx.session_id, summary, outcome)
        await _call(send_json, {"type": "call_complete", "outcome": outcome})
        await self._publisher.publish(
            "call.completed",
            json.dumps({"session_id": self.ctx.session_id, "outcome": outcome}).encode(),
        )
