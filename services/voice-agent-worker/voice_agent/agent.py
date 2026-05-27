from __future__ import annotations

import asyncio
import inspect
import json
import logging
import os
import re
import sys
import time
from typing import AsyncIterator

from voice_agent.actions import Publisher, handle_actions
from voice_agent.clients import STTClient, LLMClient, GuardrailClient, TTSClient
from voice_agent.models import SessionContext, STTResult, BrainOutput
from voice_agent.recorder import TurnStore, record_turn
from voice_agent.vad import VAD, SILENCE_THRESHOLD_MS, CHUNK_MS

_MIN_CONFIDENCE = 0.3
_END_ACTIONS = {"end_call", "opt_out"}

# Noise-token blocklist: standalone utterances that are almost certainly VAD
# artifacts / caller silence / background sound — NOT real answers.
# Only applied when confidence equals the default-fallback value (0.9) which
# batch-Sarvam returns when no real confidence is reported.
_NOISE_TOKENS: frozenset[str] = frozenset({
    "yes.", "yes", "hmm.", "hmm", "hm", "hm.", "thank you.", "thank you",
    "okay.", "okay", "ok.", "ok", "haan", "हाँ", "हाँ।", "ha", "ha.",
    "uh", "uh.", "um", "um.", "ah", "ah.",
})
# Confidence value that batch-Sarvam sets when it has NO real confidence data.
_DEFAULTED_CONFIDENCE = 0.9

# ── Barge-in debounce ─────────────────────────────────────────────────────────
# Number of consecutive SileroVAD-positive 20ms chunks required to confirm a
# real barge-in. At 20ms/chunk this is 150ms at 8 chunks and 250ms at 12.
# A single cough / click (1-2 frames) must NOT trigger; 8 chunks ≈ 160ms is
# a solid lower bound for intentional speech in human factors literature.
_BARGEIN_MIN_SPEECH_CHUNKS: int = 8  # ≈ 160ms at 20ms/chunk

# Backchannel tokens: short acknowledgements that should NOT count as real
# interrupts even after VAD confirms speech. Extends _NOISE_TOKENS with Hindi
# backchannels. If the STT result after a confirmed barge-in is ONLY one of
# these, the AI continues rather than treating it as a new utterance.
_BACKCHANNEL_TOKENS: frozenset[str] = frozenset({
    # English
    "haan", "hmm", "hmm.", "ok", "ok.", "okay", "okay.", "yes", "yes.",
    "yeah", "yeah.", "right", "right.", "sure", "sure.", "uh huh", "uh-huh",
    # Hindi / Hinglish
    "हाँ", "हाँ।", "हां", "हां।", "अच्छा", "अच्छा।", "ठीक है", "ठीक है।",
    "जी", "जी।", "जी हाँ", "जी हाँ।", "बिल्कुल", "बिल्कुल।",
    "समझ गया", "समझ गया।", "समझ गयी", "समझ गयी।",
})

# Sentence-split pattern: split after . ! ? । or when word-count ≥ 12
_SENTENCE_END = re.compile(r'(?<=[.!?।])\s+')

# ── Filler / acknowledgment phrases ──────────────────────────────────────────
# FILLER_ENABLED=false (default) — fillers are OFF by default.
# Set FILLER_ENABLED=true AND FILLER_DELAY_MS to the threshold (ms) after which
# a filler is played only if first audio hasn't started yet.
# This avoids the robotic "Hmm/Achha/Ek second" on every single turn.
_FILLER_ENABLED = os.getenv("FILLER_ENABLED", "false").lower() == "true"
# Only play a filler if first TTS audio hasn't started within this many ms
_FILLER_DELAY_MS = float(os.getenv("FILLER_DELAY_MS", "1200"))

_FILLER_TEXTS = ["Hmm", "Ji", "Achha", "Ek second"]
_filler_cache: dict[str, bytes] = {}  # text → PCM, shared across all sessions

logger = logging.getLogger(__name__)


def _split_sentences(text: str) -> list[str]:
    """Split text into sentence-sized chunks for per-sentence TTS."""
    parts = _SENTENCE_END.split(text.strip())
    return [p.strip() for p in parts if p.strip()]


def _word_count(text: str) -> int:
    return len(text.split())


def _should_flush(buffer: str) -> bool:
    """Return True if buffer should be sent to TTS now."""
    if _SENTENCE_END.search(buffer):
        return True
    if _word_count(buffer) >= 12:
        return True
    return False


async def _call(fn, *args):
    result = fn(*args)
    if inspect.isawaitable(result):
        await result


def _milestone(label: str, **kw) -> None:
    """Print a key milestone to stderr for uvicorn .err log visibility."""
    extra = " ".join(f"{k}={v}" for k, v in kw.items())
    print(f"[voice_agent] {label} {extra}", file=sys.stderr, flush=True)


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
        self._filler_index = 0  # round-robin through fillers
        # Tracks the currently-running _process_utterance Task for barge-in cancel
        self._utterance_task: asyncio.Task | None = None
        # Accumulates spoken text from interrupted turns (appended to history)
        self._interrupted_partial: str | None = None
        # Set after a barge-in so the next _process_utterance knows to check
        # for backchannel suppression
        self._next_utterance_is_bargein: bool = False

    async def _warm_fillers(self) -> None:
        """Pre-synthesize filler phrases into the module-level cache.

        Only runs when FILLER_ENABLED=true. If TTS fails, we skip gracefully.
        """
        if not _FILLER_ENABLED:
            return
        for text in _FILLER_TEXTS:
            if text in _filler_cache:
                continue
            try:
                result = await self._tts.synthesize(
                    text,
                    self.ctx.lang,
                    self.ctx.voice_profile_id,
                    self.ctx.tenant_id,
                    self.ctx.session_id,
                    self.ctx.tts_premium,
                )
                _filler_cache[text] = result.audio
            except Exception:
                logger.debug("Filler pre-synthesis failed for %r — will skip", text)

    async def _play_filler(self, send_audio: callable, first_audio_event: asyncio.Event) -> None:
        """Play a filler phrase ONLY if first TTS audio is genuinely delayed.

        Gated by FILLER_ENABLED=true. If first audio starts within
        FILLER_DELAY_MS, this no-ops — filler is never heard.
        Round-robins through _FILLER_TEXTS so consecutive fillers sound varied.
        """
        if not _FILLER_ENABLED:
            return
        # Wait for the delay threshold before playing filler
        delay_s = _FILLER_DELAY_MS / 1000.0
        try:
            # If first audio arrives in time, abort filler
            await asyncio.wait_for(first_audio_event.wait(), timeout=delay_s)
            return  # First audio already sent — no filler needed
        except asyncio.TimeoutError:
            pass  # Delay threshold hit — play filler

        text = _FILLER_TEXTS[self._filler_index % len(_FILLER_TEXTS)]
        self._filler_index += 1
        audio = _filler_cache.get(text)
        if audio and not self._stop_playback.is_set():
            try:
                await _call(send_audio, audio)
                _milestone("filler_played", session=self.ctx.session_id,
                           turn=self._turn_index, text=text)
            except Exception:
                logger.debug("Filler playback failed — continuing without filler")

    async def run(self, audio_source: AsyncIterator[bytes],
                  send_audio: callable, send_json: callable) -> BrainOutput | None:
        """Main agent loop. Returns final brain output when call ends."""
        # Pre-synthesize filler phrases only when FILLER_ENABLED=true
        if _FILLER_ENABLED:
            asyncio.create_task(self._warm_fillers())

        # Play greeting if available
        if self._greeting_audio:
            await _call(send_audio, self._greeting_audio)

        silence_chunks_needed = SILENCE_THRESHOLD_MS // CHUNK_MS
        speech_started = False
        silence_count = 0
        audio_buffer = bytearray()
        last_brain: BrainOutput | None = None

        # For streaming STT: we maintain a live queue to push chunks into
        _stt_stream_queue: asyncio.Queue[bytes | None] | None = None
        _stt_stream_task: asyncio.Task | None = None
        _stt_stream_result: list[STTResult] = []  # filled by streaming task

        async def _start_stt_stream():
            nonlocal _stt_stream_queue, _stt_stream_task, _stt_stream_result
            _stt_stream_result = []
            q: asyncio.Queue[bytes | None] = asyncio.Queue()
            _stt_stream_queue = q

            # Only start streaming if the client supports it
            if not hasattr(self._stt, "stream_transcribe"):
                return

            async def _stream_worker():
                try:
                    result = await self._stt.stream_transcribe(
                        q,
                        self.ctx.lang,
                        self.ctx.session_id,
                        getattr(self.ctx, "tenant_id", ""),
                    )
                    _stt_stream_result.append(result)
                except Exception as exc:  # noqa: BLE001
                    logger.debug("Streaming STT worker error: %r", exc)

            _stt_stream_task = asyncio.create_task(_stream_worker())

        async def _cancel_stt_stream():
            nonlocal _stt_stream_queue, _stt_stream_task
            if _stt_stream_queue is not None:
                await _stt_stream_queue.put(None)  # sentinel
                _stt_stream_queue = None
            if _stt_stream_task is not None:
                try:
                    await asyncio.wait_for(_stt_stream_task, timeout=1.0)
                except (asyncio.CancelledError, asyncio.TimeoutError, Exception):
                    _stt_stream_task.cancel()
                _stt_stream_task = None

        # Barge-in debounce counter: consecutive VAD-positive chunks during TTS
        _bargein_consec: int = 0

        async for chunk in audio_source:
            is_speech = self._vad.is_speech(chunk)

            # ── Full-duplex barge-in path ─────────────────────────────────────
            # While TTS is playing we count consecutive speech frames. Only after
            # _BARGEIN_MIN_SPEECH_CHUNKS consecutive frames do we confirm a real
            # barge-in (debounce: ignores coughs/clicks). A single non-speech
            # frame resets the counter so the caller must sustain speech.
            if self._playing_tts:
                if is_speech:
                    _bargein_consec += 1
                    if _bargein_consec >= _BARGEIN_MIN_SPEECH_CHUNKS:
                        # Confirmed barge-in — interrupt AI reply
                        _milestone("bargein_confirmed",
                                   session=self.ctx.session_id,
                                   turn=self._turn_index,
                                   consec_frames=_bargein_consec)
                        self._stop_playback.set()
                        await _call(send_json, {"type": "stop_playback"})
                        self._playing_tts = False
                        _bargein_consec = 0

                        # Cancel the utterance task (TTS will see _stop_playback)
                        if self._utterance_task is not None and not self._utterance_task.done():
                            self._utterance_task.cancel()
                            try:
                                await self._utterance_task
                            except (asyncio.CancelledError, Exception):
                                pass
                            self._utterance_task = None

                        # Append partial/interrupted reply to dialog history
                        if self._interrupted_partial:
                            self._dialog_history.append({
                                "role": "assistant",
                                "content": f"[interrupted] {self._interrupted_partial}",
                            })
                            self._interrupted_partial = None

                        # Flush in-flight STT stream; start fresh for new utterance
                        await _cancel_stt_stream()
                        audio_buffer.clear()
                        speech_started = False
                        silence_count = 0
                        self._vad.reset()

                        # Mark next utterance as post-barge-in for backchannel check
                        self._next_utterance_is_bargein = True

                        # Begin capturing the new utterance that triggered barge-in
                        await _start_stt_stream()
                        speech_started = True
                        silence_count = 0
                        audio_buffer.extend(chunk)
                        if _stt_stream_queue is not None:
                            _stt_stream_queue.put_nowait(chunk)
                    else:
                        # Accumulate the debounce chunk into buffer (will be used
                        # if/when barge-in is confirmed or if TTS ends first)
                        audio_buffer.extend(chunk)
                else:
                    # Non-speech during TTS — reset debounce counter
                    _bargein_consec = 0
                continue  # keep consuming; utterance task runs concurrently

            # ── Normal half-duplex listen path (not playing TTS) ──────────────
            _bargein_consec = 0  # reset whenever we're not in TTS

            if is_speech:
                if not speech_started:
                    # New utterance starting — kick off streaming STT
                    await _start_stt_stream()
                speech_started = True
                silence_count = 0
                audio_buffer.extend(chunk)
                # Feed chunk into STT stream
                if _stt_stream_queue is not None:
                    _stt_stream_queue.put_nowait(chunk)
            elif speech_started:
                silence_count += 1
                audio_buffer.extend(chunk)
                if silence_count >= silence_chunks_needed:
                    # Signal end-of-speech to STT stream (flush)
                    if _stt_stream_queue is not None:
                        _stt_stream_queue.put_nowait(None)  # sentinel = flush

                    # Utterance complete — run pipeline as asyncio.Task so the
                    # outer loop resumes reading audio (full-duplex: VAD runs
                    # concurrently with _process_utterance / TTS playback).
                    self._interrupted_partial = None
                    _is_bargein = self._next_utterance_is_bargein
                    self._next_utterance_is_bargein = False
                    self._utterance_task = asyncio.create_task(
                        self._process_utterance(
                            bytes(audio_buffer), send_audio, send_json,
                            stt_stream_task=_stt_stream_task,
                            stt_stream_result=_stt_stream_result,
                            is_barge_in=_is_bargein,
                        )
                    )
                    _stt_stream_queue = None
                    _stt_stream_task = None
                    audio_buffer.clear()
                    speech_started = False
                    silence_count = 0
                    self._vad.reset()

                    # Await the task here: this block only runs when NOT in TTS
                    # (we're between turns). If TTS starts mid-await the outer
                    # loop will handle it via the barge-in path above.
                    try:
                        last_brain = await self._utterance_task
                    except asyncio.CancelledError:
                        last_brain = None
                    finally:
                        self._utterance_task = None

                    if last_brain and last_brain.next_action in _END_ACTIONS:
                        break

        # If utterance task is still running (rare: stream ended mid-reply),
        # wait for it to complete cleanly.
        if self._utterance_task is not None and not self._utterance_task.done():
            try:
                last_brain = await self._utterance_task
            except (asyncio.CancelledError, Exception):
                pass
            self._utterance_task = None

        # Clean up any open stream
        await _cancel_stt_stream()

        # End of call
        await self._finalize(last_brain, send_json)
        return last_brain

    async def _process_utterance(
        self,
        audio: bytes,
        send_audio: callable,
        send_json: callable,
        *,
        stt_stream_task: asyncio.Task | None = None,
        stt_stream_result: list[STTResult] | None = None,
        is_barge_in: bool = False,
    ) -> BrainOutput:
        # t0 = speech-end (this method is invoked the moment the utterance ends)
        t0 = time.time()
        _milestone("speech_end", session=self.ctx.session_id, turn=self._turn_index)
        _skip = BrainOutput(reply="", next_action="qualify", summary="")

        # ── FILLER: only plays if TTS is genuinely slow (>FILLER_DELAY_MS) ──
        # An event is set by the TTS worker when first audio is sent.
        # _play_filler waits for that event; if TTS is fast, no filler plays.
        _first_audio_event = asyncio.Event()
        filler_task = asyncio.create_task(self._play_filler(send_audio, _first_audio_event))

        # ── STT ──────────────────────────────────────────────────────────────
        # Try streaming result first; fall back to batch.
        stt_result: STTResult | None = None
        try:
            if stt_stream_task is not None:
                # Wait for streaming STT to complete (it already has the audio)
                try:
                    await asyncio.wait_for(stt_stream_task, timeout=3.0)
                except (asyncio.TimeoutError, asyncio.CancelledError, Exception) as exc:
                    logger.debug("Streaming STT wait error: %r", exc)

                if stt_stream_result:
                    stt_result = stt_stream_result[0]
                    logger.debug("Used streaming STT result: %r", stt_result.text)

            if stt_result is None or not stt_result.text:
                # Fall back to batch STT
                stt_result = await self._stt.transcribe(
                    audio, self.ctx.lang, self.ctx.session_id
                )
        except Exception:  # noqa: BLE001
            logger.exception(
                "STT failed for session=%s turn=%s — skipping turn",
                self.ctx.session_id, self._turn_index,
            )
            filler_task.cancel()
            return _skip

        t_stt = time.time()
        stt_ms = (t_stt - t0) * 1000
        logger.info(
            "latency session=%s turn=%s leg=stt ms=%.0f",
            self.ctx.session_id, self._turn_index, stt_ms,
        )
        _milestone("stt_final", session=self.ctx.session_id, turn=self._turn_index,
                   ms=f"{stt_ms:.0f}", text=repr(stt_result.text[:40] if stt_result.text else ""))

        # ── Noise gate ────────────────────────────────────────────────────────
        # 1) Reject empty / whitespace-only transcript.
        # 2) Reject degenerate noise tokens when confidence is the batch-Sarvam
        #    default (0.9) — meaning no real confidence was returned.
        #    Real engines (streaming saaras:v3) report lower, calibrated values
        #    so a genuine short Hindi answer will not be dropped here.
        # 3) Keep the hard _MIN_CONFIDENCE floor for engines with real scores.
        _text_stripped = (stt_result.text or "").strip()
        _is_noise_token = (
            _text_stripped.lower() in _NOISE_TOKENS
            and abs(stt_result.confidence - _DEFAULTED_CONFIDENCE) < 1e-6
        )
        if not _text_stripped:
            print(
                f"[voice_agent] turn_skipped_noise reason=empty"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        if _is_noise_token:
            print(
                f"[voice_agent] turn_skipped_noise reason=noise_token"
                f" text={_text_stripped!r} conf={stt_result.confidence:.3f}"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        if stt_result.confidence < _MIN_CONFIDENCE:
            print(
                f"[voice_agent] turn_skipped_noise reason=low_confidence"
                f" text={_text_stripped!r} conf={stt_result.confidence:.3f}"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        # ── /Noise gate ───────────────────────────────────────────────────────

        # ── Backchannel suppression (barge-in only) ───────────────────────────
        # If the caller interrupted a playing reply but the transcript is only
        # a short acknowledgement ("haan", "hmm", "अच्छा" etc.), treat it as a
        # backchannel — do NOT issue a new AI reply. The caller's "haan" means
        # "I hear you, keep going" rather than "stop and answer me".
        if is_barge_in and _text_stripped.lower() in _BACKCHANNEL_TOKENS:
            _milestone("bargein_suppressed_backchannel",
                       session=self.ctx.session_id,
                       turn=self._turn_index,
                       text=repr(_text_stripped))
            print(
                f"[voice_agent] turn_skipped_bargein_backchannel"
                f" text={_text_stripped!r}"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        # ── /Backchannel suppression ──────────────────────────────────────────

        # Ensure filler has been sent before starting the real reply
        try:
            await asyncio.wait_for(filler_task, timeout=0.5)
        except (asyncio.TimeoutError, asyncio.CancelledError, Exception):
            pass  # filler is best-effort

        # ── LLM streaming + per-sentence TTS ─────────────────────────────────
        self._stop_playback.clear()
        self._playing_tts = True
        brain: BrainOutput | None = None

        try:
            brain = await self._run_streaming_llm_tts(
                stt_result, send_audio, t_stt, _first_audio_event
            )
        except Exception:  # noqa: BLE001
            self._playing_tts = False
            logger.exception(
                "Streaming LLM+TTS failed for session=%s turn=%s — skipping turn",
                self.ctx.session_id, self._turn_index,
            )
            return _skip
        finally:
            self._playing_tts = False

        if brain is None:
            return _skip

        t_done = time.time()
        _milestone("reply_done", session=self.ctx.session_id, turn=self._turn_index,
                   total_ms=f"{(t_done - t0) * 1000:.0f}")
        logger.info(
            "latency session=%s turn=%s leg=total ms=%.0f",
            self.ctx.session_id, self._turn_index, (t_done - t0) * 1000,
        )

        # Record turn
        await record_turn(
            self._store, self.ctx, self._turn_index,
            stt_result, brain, "", False,
        )

        # Actions
        await handle_actions(brain, self.ctx, self._publisher)

        # History
        self._dialog_history.append({"role": "user", "content": stt_result.text})
        self._dialog_history.append({"role": "assistant", "content": brain.reply})
        self._turn_index += 1

        return brain

    async def _run_streaming_llm_tts(
        self,
        stt_result: STTResult,
        send_audio: callable,
        t_stt: float,
        first_audio_event: asyncio.Event | None = None,
    ) -> BrainOutput | None:
        """Stream LLM tokens → accumulate sentences → per-sentence TTS → play.

        Strategy (fast path — uses plain-text stream):
        - Start plain-text stream (generate_stream_text) for immediate speech tokens
        - Start parallel batch generate() call for structured metadata (non-blocking)
        - Accumulate plain-text tokens into sentence chunks, TTS each immediately
        - After speech finishes, await metadata result for next_action/summary/lead fields
        - If plain-text stream unavailable or fails → fall back to old JSON stream path
        - If parallel metadata call fails → default next_action="qualify"

        Returns the final BrainOutput or None on unrecoverable error.
        """
        use_stream_text = hasattr(self._llm, "generate_stream_text")
        brain_out: BrainOutput | None = None

        if use_stream_text:
            try:
                brain_out = await self._stream_text_path(stt_result, send_audio, t_stt, first_audio_event)
                return brain_out
            except Exception as exc:
                logger.warning(
                    "Plain-text stream path failed (%r) — falling back for session=%s",
                    exc, self.ctx.session_id,
                )
                # Fall through to JSON stream path

        # ── Fallback: old JSON streaming path ─────────────────────────────────
        use_stream = hasattr(self._llm, "generate_stream")
        if use_stream:
            try:
                brain_out = await self._stream_path(stt_result, send_audio, t_stt, first_audio_event)
                return brain_out
            except Exception as exc:
                logger.warning(
                    "LLM stream path failed (%r) — falling back to batch for session=%s",
                    exc, self.ctx.session_id,
                )

        # ── Batch fallback ────────────────────────────────────────────────────
        brain_out = await self._batch_path(stt_result, send_audio, t_stt, first_audio_event)
        return brain_out

    async def _stream_text_path(
        self,
        stt_result: STTResult,
        send_audio: callable,
        t_stt: float,
        first_audio_event: asyncio.Event | None = None,
    ) -> BrainOutput:
        """Fast path: plain-text LLM stream for speech + parallel batch for metadata.

        1. Fire generate_stream_text (no JSON mode) — ~0.8s first-token
        2. Simultaneously fire generate() (batch, JSON) for metadata — runs in background
        3. Accumulate plain-text tokens into sentence chunks, TTS + play each immediately
        4. After speech is done, await metadata result; merge spoken text into brain
        5. If metadata fails, synthesize a minimal BrainOutput from the spoken text

        The caller hears the reply starting ~0.8s + TTS latency after speech_end.
        next_action, lead_score, summary etc. come from the parallel metadata call.
        """
        token_buffer = ""
        full_spoken = ""
        first_audio_sent = False

        # ── Start parallel metadata call (non-blocking) ────────────────────────
        metadata_task: asyncio.Task = asyncio.create_task(
            self._llm.generate(self.ctx, stt_result.text, self._dialog_history)
        )

        # ── TTS worker (same sentence-queue pattern as _stream_path) ──────────
        tts_queue: asyncio.Queue[tuple[str, bool]] = asyncio.Queue()

        async def _tts_worker():
            nonlocal first_audio_sent
            while True:
                sentence, is_done = await tts_queue.get()
                if is_done and not sentence:
                    break
                if not sentence:
                    continue
                if self._stop_playback.is_set():
                    break
                try:
                    tts_result = await self._tts.synthesize(
                        sentence,
                        self.ctx.lang,
                        self.ctx.voice_profile_id,
                        self.ctx.tenant_id,
                        self.ctx.session_id,
                        self.ctx.tts_premium,
                    )
                    if not self._stop_playback.is_set():
                        t_audio = time.time()
                        if not first_audio_sent:
                            first_audio_sent = True
                            # Signal filler task that audio is ready — no filler needed
                            if first_audio_event is not None:
                                first_audio_event.set()
                            _milestone(
                                "first_audio_sent",
                                session=self.ctx.session_id,
                                ms=f"{(t_audio - t_stt) * 1000:.0f}",
                            )
                            logger.info(
                                "latency session=%s turn=%s leg=first_audio ms=%.0f",
                                self.ctx.session_id, self._turn_index,
                                (t_audio - t_stt) * 1000,
                            )
                        await _call(send_audio, tts_result.audio)
                except Exception:  # noqa: BLE001
                    logger.exception(
                        "TTS failed for sentence=%r session=%s",
                        sentence[:40], self.ctx.session_id,
                    )

        tts_task = asyncio.create_task(_tts_worker())

        try:
            # ── Consume plain-text token stream ────────────────────────────────
            first_token_logged = False
            async for token, _ in self._llm.generate_stream_text(
                self.ctx, stt_result.text, self._dialog_history
            ):
                if not first_token_logged and token:
                    first_token_logged = True
                    _milestone(
                        "llm_first_token",
                        session=self.ctx.session_id,
                        turn=self._turn_index,
                        ms=f"{(time.time() - t_stt) * 1000:.0f}",
                    )

                if token:
                    token_buffer += token
                    full_spoken += token

                    if _should_flush(token_buffer):
                        sentence = token_buffer.strip()
                        token_buffer = ""
                        if sentence:
                            await tts_queue.put((sentence, False))

                # Barge-in during LLM streaming: record partial and abort
                if self._stop_playback.is_set():
                    self._interrupted_partial = full_spoken.strip() or None
                    break

            # Flush any remaining buffer
            if not self._stop_playback.is_set() and token_buffer.strip():
                await tts_queue.put((token_buffer.strip(), False))
            await tts_queue.put(("", True))  # signal TTS worker done

            # Wait for TTS to finish playing
            await tts_task

        except (asyncio.CancelledError, Exception):
            # On cancellation (barge-in): record partial spoken text for history
            if full_spoken.strip():
                self._interrupted_partial = full_spoken.strip()
            tts_task.cancel()
            try:
                await tts_task
            except (asyncio.CancelledError, Exception):
                pass
            metadata_task.cancel()
            raise

        # ── Await metadata (should be done or nearly done by now) ─────────────
        meta_brain: BrainOutput | None = None
        try:
            meta_brain = await asyncio.wait_for(metadata_task, timeout=5.0)
        except (asyncio.TimeoutError, Exception) as exc:
            logger.warning(
                "Parallel metadata call failed (%r) for session=%s — using defaults",
                exc, self.ctx.session_id,
            )
            metadata_task.cancel()

        # ── Guardrail on metadata brain (quick) ────────────────────────────────
        if meta_brain is not None:
            try:
                meta_brain = await asyncio.wait_for(
                    asyncio.shield(
                        asyncio.ensure_future(
                            self._guardrail.check(meta_brain, [], stt_result.text)
                        )
                    ),
                    timeout=1.5,
                )
            except (asyncio.TimeoutError, Exception) as exc:
                logger.warning("Guardrail check failed (%r) — using raw meta_brain", exc)

        # ── Build final BrainOutput: spoken text + metadata fields ─────────────
        spoken_text = full_spoken.strip()
        if meta_brain is not None:
            # Use metadata for all structured fields; override reply with what was spoken
            brain_out = meta_brain.model_copy(update={"reply": spoken_text or meta_brain.reply})
        else:
            # Metadata failed — synthesize minimal brain from spoken text
            _fallback_reply = spoken_text or "Hum aapki baat ek specialist ko transfer kar rahe hain."
            brain_out = BrainOutput(
                reply=_fallback_reply,
                lead_status="needs_human_review",
                lead_score=0,
                next_action="qualify",
                should_handover_to_human=False,
                risk_level="safe",
                confidence=0.5,
                summary=f"Metadata unavailable. Spoken: {spoken_text[:80]}",
            )

        logger.info(
            "latency session=%s turn=%s leg=stream_text_total ms=%.0f",
            self.ctx.session_id, self._turn_index,
            (time.time() - t_stt) * 1000,
        )
        return brain_out

    async def _stream_path(
        self,
        stt_result: STTResult,
        send_audio: callable,
        t_stt: float,
        first_audio_event: asyncio.Event | None = None,
    ) -> BrainOutput:
        """
        LLM stream → sentence-chunk TTS → play immediately.
        Guardrail runs concurrently with TTS of each sentence.
        """
        token_buffer = ""
        full_reply = ""
        first_audio_sent = False
        brain_out: BrainOutput | None = None

        # Queue of (sentence_text, is_final_brain) for the TTS coroutine
        tts_queue: asyncio.Queue[tuple[str, bool]] = asyncio.Queue()
        # Guardrail results indexed by sentence position
        guardrail_tasks: list[asyncio.Task] = []

        async def _tts_worker():
            nonlocal first_audio_sent
            while True:
                item = await tts_queue.get()
                sentence, is_done = item
                if is_done and not sentence:
                    break
                if not sentence:
                    continue
                if self._stop_playback.is_set():
                    break
                try:
                    tts_result = await self._tts.synthesize(
                        sentence,
                        self.ctx.lang,
                        self.ctx.voice_profile_id,
                        self.ctx.tenant_id,
                        self.ctx.session_id,
                        self.ctx.tts_premium,
                    )
                    if not self._stop_playback.is_set():
                        t_audio = time.time()
                        if not first_audio_sent:
                            first_audio_sent = True
                            # Signal filler task that audio is ready — no filler needed
                            if first_audio_event is not None:
                                first_audio_event.set()
                            _milestone(
                                "first_audio_sent",
                                session=self.ctx.session_id,
                                ms=f"{(t_audio - t_stt) * 1000:.0f}",
                            )
                            logger.info(
                                "latency session=%s turn=%s leg=first_audio ms=%.0f",
                                self.ctx.session_id, self._turn_index,
                                (t_audio - t_stt) * 1000,
                            )
                        await _call(send_audio, tts_result.audio)
                except Exception:  # noqa: BLE001
                    logger.exception(
                        "TTS failed for sentence=%r session=%s",
                        sentence[:40], self.ctx.session_id,
                    )

        tts_task = asyncio.create_task(_tts_worker())

        try:
            async for token, final_brain in self._llm.generate_stream(
                self.ctx, stt_result.text, self._dialog_history
            ):
                if final_brain is not None:
                    # Stream done — run guardrail
                    brain_out = final_brain
                    try:
                        brain_out = await asyncio.wait_for(
                            asyncio.shield(
                                asyncio.ensure_future(
                                    self._guardrail.check(
                                        final_brain, [], stt_result.text
                                    )
                                )
                            ),
                            timeout=1.5,
                        )
                    except (asyncio.TimeoutError, Exception) as exc:
                        logger.warning(
                            "Guardrail check timed out/failed (%r), using raw brain",
                            exc,
                        )
                        brain_out = final_brain

                    # Flush any remaining buffer as last sentence
                    if token_buffer.strip():
                        await tts_queue.put((token_buffer.strip(), False))
                    # Signal TTS worker to finish
                    await tts_queue.put(("", True))
                    break

                # Accumulate token
                if token:
                    token_buffer += token
                    full_reply += token

                    if _should_flush(token_buffer):
                        sentence = token_buffer.strip()
                        token_buffer = ""
                        if sentence:
                            await tts_queue.put((sentence, False))

            # Wait for TTS worker to drain
            await tts_task

        except Exception:
            tts_task.cancel()
            try:
                await tts_task
            except (asyncio.CancelledError, Exception):
                pass
            raise

        if brain_out is None:
            raise RuntimeError("LLM stream ended without final brain output")

        # Update brain reply with full accumulated text if the stream brain has empty reply
        if not brain_out.reply and full_reply:
            brain_out = brain_out.model_copy(update={"reply": full_reply.strip()})

        logger.info(
            "latency session=%s turn=%s leg=llm_stream_total ms=%.0f",
            self.ctx.session_id, self._turn_index,
            (time.time() - t_stt) * 1000,
        )
        return brain_out

    async def _batch_path(
        self,
        stt_result: STTResult,
        send_audio: callable,
        t_stt: float,
        first_audio_event: asyncio.Event | None = None,
    ) -> BrainOutput:
        """Batch LLM + guardrail + per-sentence TTS (fallback path)."""
        # LLM
        try:
            brain = await self._llm.generate(
                self.ctx, stt_result.text, self._dialog_history
            )
        except Exception:  # noqa: BLE001
            logger.exception(
                "Batch LLM failed for session=%s turn=%s",
                self.ctx.session_id, self._turn_index,
            )
            raise

        t_llm = time.time()
        logger.info(
            "latency session=%s turn=%s leg=llm ms=%.0f",
            self.ctx.session_id, self._turn_index, (t_llm - t_stt) * 1000,
        )
        _milestone("llm_first_token", session=self.ctx.session_id,
                   turn=self._turn_index, ms=f"{(t_llm - t_stt) * 1000:.0f}")

        # Guardrail — run concurrently with TTS of first sentence
        guardrail_task = asyncio.create_task(
            self._guardrail.check(brain, [], stt_result.text)
        )

        # TTS — split reply into sentences, synthesize + play each immediately
        sentences = _split_sentences(brain.reply) or [brain.reply]
        first_audio_sent = False

        for i, sentence in enumerate(sentences):
            if self._stop_playback.is_set():
                break
            if not sentence.strip():
                continue
            try:
                tts_result = await self._tts.synthesize(
                    sentence,
                    self.ctx.lang,
                    self.ctx.voice_profile_id,
                    self.ctx.tenant_id,
                    self.ctx.session_id,
                    self.ctx.tts_premium,
                )
                if not self._stop_playback.is_set():
                    t_audio = time.time()
                    if not first_audio_sent:
                        first_audio_sent = True
                        # Signal filler task that audio is ready — no filler needed
                        if first_audio_event is not None:
                            first_audio_event.set()
                        _milestone(
                            "first_audio_sent",
                            session=self.ctx.session_id,
                            ms=f"{(t_audio - t_stt) * 1000:.0f}",
                        )
                        logger.info(
                            "latency session=%s turn=%s leg=first_audio ms=%.0f",
                            self.ctx.session_id, self._turn_index,
                            (t_audio - t_stt) * 1000,
                        )
                    await _call(send_audio, tts_result.audio)
            except Exception:  # noqa: BLE001
                logger.exception(
                    "TTS failed for sentence=%r session=%s",
                    sentence[:40], self.ctx.session_id,
                )

        # Await guardrail (should already be done by the time TTS finishes)
        try:
            brain = await asyncio.wait_for(guardrail_task, timeout=1.5)
        except (asyncio.TimeoutError, Exception) as exc:
            logger.warning(
                "Guardrail check timed out/failed (%r) for session=%s, using raw brain",
                exc, self.ctx.session_id,
            )

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
