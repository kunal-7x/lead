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

# Single-word legitimate Hindi answers that must NOT be dropped even when
# they appear alone with defaulted confidence.
_VALID_HINDI_SINGLE_WORDS: frozenset[str] = frozenset({
    "हाँ", "हां", "नहीं", "नही", "जी", "हाँ।", "नहीं।",
    # common budget / location single-token answers are NOT ASCII, so they pass
    # the ASCII guard automatically — listed here for documentation only.
})

# Max byte length for a single-word OOV English token to be noise-gated.
# "Sir" = 3 chars. We gate ≤8 chars to catch "Sir", "Hmm", "Yeah", "Thanks" etc.
_MAX_OOV_ENGLISH_TOKEN_LEN = 8


def _is_oov_english_noise(text_stripped: str, confidence: float) -> bool:
    """Return True if the transcript looks like a stray English background token.

    Criteria (ALL must hold):
    - Entire transcript is a single word (no spaces after stripping punctuation).
    - Word is pure ASCII (not Hindi/Devanagari — those are multi-byte UTF-8).
    - Word length ≤ _MAX_OOV_ENGLISH_TOKEN_LEN.
    - Confidence equals the Sarvam batch default (0.9) — meaning no real score.
    - Word is NOT a legitimate single-word Hindi answer (paranoia guard).

    Keeps real Hindi answers safe because Devanagari is non-ASCII.
    """
    if abs(confidence - _DEFAULTED_CONFIDENCE) >= 1e-6:
        return False  # has a real confidence score — trust it
    # Strip trailing punctuation for word check
    word = text_stripped.rstrip(".!?,।").strip()
    if not word or " " in word:
        return False  # multi-word or empty — not this check's concern
    try:
        word.encode("ascii")
    except UnicodeEncodeError:
        return False  # non-ASCII → Hindi/Devanagari → keep it
    if word.lower() in _VALID_HINDI_SINGLE_WORDS:
        return False
    return len(word) <= _MAX_OOV_ENGLISH_TOKEN_LEN

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
# Sarvam streaming-TTS WS path (HIGH risk — touches real-time audio). DEFAULT OFF.
# When true, _tts_worker consumes streaming PCM chunks and feeds send_audio as they
# arrive (first-audio ~0.3s). When false, the batch synthesize() path is used UNCHANGED.
# On any streaming error the worker transparently falls back to batch synthesize().
_TTS_STREAMING_WS = os.getenv("TTS_STREAMING_WS", "false").lower() == "true"
# Only play a filler if first TTS audio hasn't started within this many ms after speech_end.
# 2500 ms: conservative threshold — filler should only fire on genuinely very slow turns.
_FILLER_GAP_MS = float(os.getenv("FILLER_GAP_MS", "2500"))
# Legacy alias — FILLER_DELAY_MS is still accepted but FILLER_GAP_MS takes priority if set.
_FILLER_DELAY_MS = float(os.getenv("FILLER_GAP_MS", os.getenv("FILLER_DELAY_MS", "2500")))

# Short, natural Hindi fillers (<1 s audio each). Rotated round-robin.
_FILLER_TEXTS = ["जी...", "हाँ जी", "एक सेकंड", "जी बिल्कुल"]
_filler_cache: dict[str, bytes] = {}  # text → PCM, shared across all sessions

logger = logging.getLogger(__name__)


def _update_accumulated_slots(slots: dict, brain: "BrainOutput") -> None:
    """Merge non-null fields from brain into the accumulated slots dict.

    Called after each turn's metadata brain is received. Only overwrites a slot
    if the new value is non-None (never clears a previously set slot).
    brain.budget is a dict (as deserialized by the worker's simpler BrainOutput).
    """
    budget = brain.budget if isinstance(brain.budget, dict) else None
    if budget:
        if budget.get("text"):
            slots["budget_text"] = budget["text"]
        if budget.get("value") is not None:
            slots["budget_value"] = budget["value"]
    if brain.location_pref is not None:
        slots["location_pref"] = brain.location_pref
    if brain.property_type is not None:
        slots["property_type"] = brain.property_type
    if brain.timeline_days is not None:
        slots["timeline_days"] = brain.timeline_days
    if brain.purpose is not None:
        slots["purpose"] = brain.purpose
    if brain.summary:
        slots["summary"] = brain.summary


def _split_sentences(text: str) -> list[str]:
    """Split text into sentence-sized chunks for per-sentence TTS."""
    parts = _SENTENCE_END.split(text.strip())
    return [p.strip() for p in parts if p.strip()]


def _word_count(text: str) -> int:
    return len(text.split())


_CHUNK_MIN_WORDS = 12   # below this, hold and join with next clause (avoid choppy packets)
_CHUNK_MAX_WORDS = 35   # force-flush regardless of punctuation (human breath window)
_CHUNK_FIRST_WORDS = 8  # flush first chunk earlier to keep first-audio latency low

def _should_flush(buffer: str, is_first_chunk: bool = False, llm_done: bool = False) -> bool:
    """Breath-rhythm chunking: flush on sentence-end + minimum word threshold.

    - Force-flush at MAX words (hard ceiling).
    - First chunk: flush at first sentence-end with >=8 words for low first-audio latency.
    - Subsequent chunks: flush on sentence-end only if >=12 words (thought-group buffering).
      If buffer is under MIN and LLM stream is still active, HOLD — join with next clause.
    - Always flush when LLM stream ends (llm_done=True) regardless of word count.
    - Never split on comma alone; a lone comma-clause joins the next sentence.

    [diag] phase=chunk info is logged by the caller with actual sizes.
    """
    wc = _word_count(buffer)
    if wc >= _CHUNK_MAX_WORDS:
        return True
    if llm_done:
        return bool(buffer.strip())
    if _SENTENCE_END.search(buffer):
        threshold = _CHUNK_FIRST_WORDS if is_first_chunk else _CHUNK_MIN_WORDS
        return wc >= threshold
    return False


async def _call(fn, *args):
    result = fn(*args)
    if inspect.isawaitable(result):
        await result


def _milestone(label: str, **kw) -> None:
    """Print a key milestone to stderr for uvicorn .err log visibility."""
    extra = " ".join(f"{k}={v}" for k, v in kw.items())
    print(f"[voice_agent] {label} {extra}", file=sys.stderr, flush=True)


def _diag(session: str, turn: int, **kw) -> None:
    """Emit a single greppable diagnostic line to stderr.

    Format: [diag] session=X turn=N key=val ...
    Grep 'session=X' to reconstruct a full call, '[diag]' for all diag lines.
    Values are kept concise — no audio bytes, no secrets.
    """
    extra = " ".join(f"{k}={v}" for k, v in kw.items())
    print(f"[diag] session={session} turn={turn} {extra}", file=sys.stderr, flush=True)


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
        # Accumulated slot values across the call — updated after each turn that
        # returns a meta_brain. Passed to LLM as collected_slots to prevent re-asking.
        self._accumulated_slots: dict = {}
        # Bug A guard: filler must NOT fire until at least one real user utterance
        # has passed the noise gate. Greeting-window noise and sub-threshold VAD
        # blips must never trigger filler. Set to True the first time noise gate
        # accepts a user turn; never reset (filler stays eligible for the whole call).
        self._real_utterance_seen: bool = False

    async def _synth_and_play_stream(self, sentence: str, send_audio: callable) -> bool:
        """Stream one sentence via Sarvam WS, feeding chunks to send_audio as they
        arrive. Returns True if streaming produced audio; False if it should fall
        back to batch (error / no client support / disabled).

        Barge-in: each chunk is gated on _stop_playback; send_audio itself also
        aborts mid-frame within ~20ms. On barge-in we stop consuming immediately.
        """
        if not _TTS_STREAMING_WS or not hasattr(self._tts, "synthesize_stream"):
            return False
        produced = False
        try:
            async for pcm_chunk in self._tts.synthesize_stream(
                sentence, self.ctx.lang, self.ctx.voice_profile_id,
            ):
                if self._stop_playback.is_set():
                    # Barge-in mid-stream: stop consuming/sending. send_json
                    # clearAudio is driven by the existing barge-in handler.
                    break
                if pcm_chunk:
                    produced = True
                    await _call(send_audio, pcm_chunk)
            return produced
        except Exception as exc:  # noqa: BLE001
            # streaming_disabled or WS failure → caller falls back to batch.
            logger.warning(
                "TTS streaming failed (%r) — falling back to batch for session=%s",
                exc, self.ctx.session_id,
            )
            return False

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

    async def _play_filler(self, send_audio: callable, first_audio_event: asyncio.Event,
                           t_speech_end: float | None = None,
                           noise_gate_event: asyncio.Event | None = None) -> None:
        """Play a filler phrase ONLY if first TTS audio is genuinely delayed.

        Gap-triggered: waits _FILLER_GAP_MS after speech_end. If first audio
        arrives in time the filler is skipped silently (fast turns = no filler).

        Noise-gate guard: filler is only played if THIS TURN passes the noise
        gate. A per-turn noise_gate_event is set by _process_utterance exactly
        when the noise gate accepts the transcript (real turn). On noise/skipped
        turns the caller cancels filler_task before setting the event, so filler
        never plays. This replaces the stale _real_utterance_seen call-level flag
        which caused: (a) filler blocked on the first real slow turn because the
        flag was still False, and (b) filler firing on later noise turns because
        the flag was True from a previous real turn.

        At most once per turn — structurally guaranteed: exactly one filler_task
        is created per _process_utterance call. The task is cancelled on all
        early-exit paths (noise-gate reject, backchannel suppression, STT error).
        On each new turn the caller creates a fresh filler_task, so filler fires
        once per slow turn for the entire call (per-turn, not per-call).

        Round-robins through _FILLER_TEXTS so consecutive fillers sound varied.
        """
        if not _FILLER_ENABLED:
            return
        gap_s = _FILLER_GAP_MS / 1000.0
        t_start = t_speech_end if t_speech_end is not None else time.time()
        try:
            # If first audio arrives within the gap, abort — no filler needed.
            await asyncio.wait_for(first_audio_event.wait(), timeout=gap_s)
            gap_ms = (time.time() - t_start) * 1000
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler", status="skipped",
                  reason="first_audio_fast", gap_ms=f"{gap_ms:.0f}")
            return
        except asyncio.TimeoutError:
            pass  # Gap threshold hit — wait for noise-gate verdict

        gap_ms = (time.time() - t_start) * 1000

        # Per-turn noise-gate guard: wait for this turn's noise gate to accept
        # the transcript. The event is set only when the transcript passes all
        # noise-gate checks. On noise/empty/backchannel turns the filler_task is
        # cancelled by the caller before this event is set, so we never reach here.
        # Timeout: 2 s max wait (STT + noise-gate should finish well within that;
        # if something hangs we bail rather than block the call indefinitely).
        if noise_gate_event is not None:
            try:
                await asyncio.wait_for(noise_gate_event.wait(), timeout=2.0)
            except asyncio.TimeoutError:
                _diag(self.ctx.session_id, self._turn_index,
                      phase="filler", status="skipped",
                      reason="noise_gate_timeout", gap_ms=f"{gap_ms:.0f}")
                return
        elif not self._real_utterance_seen:
            # Fallback for callers that don't pass noise_gate_event (legacy path).
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler", status="skipped",
                  reason="pre_utterance_lockout", gap_ms=f"{gap_ms:.0f}")
            return

        text = _FILLER_TEXTS[self._filler_index % len(_FILLER_TEXTS)]
        self._filler_index += 1
        audio = _filler_cache.get(text)
        if audio and not self._stop_playback.is_set():
            try:
                # Arm barge-in gate while filler audio is playing so callers can
                # interrupt the filler just like they can interrupt the main reply.
                self._playing_tts = True
                await _call(send_audio, audio)
                _milestone("filler_played", session=self.ctx.session_id,
                           turn=self._turn_index, text=text)
                _diag(self.ctx.session_id, self._turn_index,
                      phase="filler", status="played",
                      reason="first_audio_delayed", gap_ms=f"{gap_ms:.0f}",
                      text=repr(text))
            except Exception:
                logger.debug("Filler playback failed — continuing without filler")
                _diag(self.ctx.session_id, self._turn_index,
                      phase="filler", status="skipped",
                      reason="playback_error", gap_ms=f"{gap_ms:.0f}")
        elif self._stop_playback.is_set():
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler", status="skipped",
                  reason="barge_in", gap_ms=f"{gap_ms:.0f}")

    async def run(self, audio_source: AsyncIterator[bytes],
                  send_audio: callable, send_json: callable) -> BrainOutput | None:
        """Main agent loop. Returns final brain output when call ends."""
        # Pre-synthesize filler phrases only when FILLER_ENABLED=true
        if _FILLER_ENABLED:
            asyncio.create_task(self._warm_fillers())

        # Play greeting if available.
        # Set _playing_tts=True so that the audio loop below (which starts
        # reading frames immediately) treats incoming ambient noise as
        # potential barge-in rather than a new user utterance.  This kills
        # the 3×3-4 s cold-start batch-STT round-trips on "Yes."/"Hello"/"".
        # Barge-in still works: if the caller speaks ≥8 consecutive VAD-
        # positive frames (~160 ms) OVER the greeting, _BARGEIN_MIN_SPEECH_CHUNKS
        # fires and interrupts it correctly.
        if self._greeting_audio:
            self._playing_tts = True
            self._stop_playback.clear()
            await _call(send_audio, self._greeting_audio)
            # Greeting finished (not interrupted) — allow normal listen loop.
            if not self._stop_playback.is_set():
                self._playing_tts = False
                _diag(self.ctx.session_id, 0,
                      phase="greeting", status="complete",
                      audio_bytes=len(self._greeting_audio))

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
            # If a stream is already running (eager pre-connect), reuse it.
            # This avoids creating a duplicate WS connection on first speech.
            if _stt_stream_queue is not None and _stt_stream_task is not None and not _stt_stream_task.done():
                return
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
        # Speech chunk counter for diag (reset each utterance)
        _speech_chunk_count: int = 0

        # Eager STT stream pre-connect: start the streaming WebSocket NOW, before
        # the first real speech chunk arrives. This ensures saaras:v3 is already
        # connected when the caller starts speaking so turn-0 never falls back to
        # the slow batch-Sarvam path.
        if hasattr(self._stt, "stream_transcribe"):
            await _start_stt_stream()
            _diag(self.ctx.session_id, 0,
                  phase="stt_preconnect", status="started",
                  engine="saaras:v3")

        async for chunk in audio_source:
            # ── Collect completed utterance task result (non-blocking) ─────────
            # The utterance task runs concurrently (full-duplex). We poll its
            # done-state every iteration so we capture last_brain and can detect
            # _END_ACTIONS without blocking the audio loop.
            if self._utterance_task is not None and self._utterance_task.done():
                try:
                    _result = self._utterance_task.result()
                    if _result is not None:
                        last_brain = _result
                        if last_brain.next_action in _END_ACTIONS:
                            break
                except (asyncio.CancelledError, Exception):
                    pass
                self._utterance_task = None

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
                        _speech_chunk_count = 1  # this chunk is the first of the new utterance
                        audio_buffer.extend(chunk)
                        if _stt_stream_queue is not None:
                            _stt_stream_queue.put_nowait(chunk)
                    else:
                        # Accumulate the debounce chunk into buffer (will be used
                        # if/when barge-in is confirmed or if TTS ends first)
                        audio_buffer.extend(chunk)
                        # Diagnostic: brief noise during greeting suppressed (turn 0 only)
                        if self._turn_index == 0 and _bargein_consec == 1:
                            _diag(self.ctx.session_id, 0,
                                  phase="stt_suppressed_greeting",
                                  reason="noise_during_greeting",
                                  consec_frames=_bargein_consec)
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
                    _speech_chunk_count = 0
                speech_started = True
                silence_count = 0
                _speech_chunk_count += 1
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
                    _utterance_chunk_count = _speech_chunk_count
                    self._utterance_task = asyncio.create_task(
                        self._process_utterance(
                            bytes(audio_buffer), send_audio, send_json,
                            stt_stream_task=_stt_stream_task,
                            stt_stream_result=_stt_stream_result,
                            is_barge_in=_is_bargein,
                            speech_chunk_count=_utterance_chunk_count,
                        )
                    )
                    _stt_stream_queue = None
                    _stt_stream_task = None
                    _speech_chunk_count = 0
                    audio_buffer.clear()
                    speech_started = False
                    silence_count = 0
                    self._vad.reset()

                    # Do NOT await here — continue consuming audio so the
                    # barge-in path (above) can fire while TTS is playing.
                    # The task result is collected at the top of the loop once done.

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
        speech_chunk_count: int = 0,
    ) -> BrainOutput:
        # t0 = speech-end (this method is invoked the moment the utterance ends)
        t0 = time.time()
        _speech_duration_ms = speech_chunk_count * CHUNK_MS
        _milestone("speech_end", session=self.ctx.session_id, turn=self._turn_index)
        _diag(self.ctx.session_id, self._turn_index,
              phase="speech",
              chunks=speech_chunk_count,
              speech_ms=_speech_duration_ms,
              audio_bytes=len(audio),
              barge_in=is_barge_in)
        _skip = BrainOutput(reply="", next_action="qualify", summary="")

        # ── FILLER: only plays if TTS is genuinely slow (>FILLER_DELAY_MS) ──
        # An event is set by the TTS worker when first audio is sent.
        # _play_filler waits for that event; if TTS is fast, no filler plays.
        # noise_gate_event: set only when THIS turn passes the noise gate.
        # Replaces the stale _real_utterance_seen call-level flag that caused
        # filler to fire on noise turns (flag=True from prior real turn) and to
        # be silenced on the first real slow turn (flag still False at 400ms).
        _first_audio_event = asyncio.Event()
        _noise_gate_event = asyncio.Event()
        filler_task = asyncio.create_task(
            self._play_filler(send_audio, _first_audio_event, t0, _noise_gate_event)
        )

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
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler_decision", turn_type="noise",
                  reason="stt_error", msg="filler_cancelled")
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
        _stt_engine = stt_result.engine_used or ("streaming" if stt_stream_task is not None and stt_result.text else "batch")
        _diag(self.ctx.session_id, self._turn_index,
              phase="stt",
              engine=_stt_engine,
              lang=self.ctx.lang,
              conf=f"{stt_result.confidence:.3f}",
              ms=f"{stt_ms:.0f}",
              text=repr((stt_result.text or "")[:60]))

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
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler_decision", turn_type="noise",
                  reason="empty_transcript", msg="filler_cancelled")
            print(
                f"[voice_agent] turn_skipped_noise reason=empty"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        if _is_noise_token:
            _diag(self.ctx.session_id, self._turn_index,
                  phase="noise_gate", reason="noise_token",
                  text=repr(_text_stripped), conf=f"{stt_result.confidence:.3f}")
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler_decision", turn_type="noise",
                  reason="noise_token", msg="filler_cancelled")
            print(
                f"[voice_agent] turn_skipped_noise reason=noise_token"
                f" text={_text_stripped!r} conf={stt_result.confidence:.3f}"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        _is_oov_english = _is_oov_english_noise(_text_stripped, stt_result.confidence)
        if _is_oov_english:
            _diag(self.ctx.session_id, self._turn_index,
                  phase="noise_gate", reason="oov_english_token",
                  text=repr(_text_stripped), conf=f"{stt_result.confidence:.3f}")
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler_decision", turn_type="noise",
                  reason="oov_english_token", msg="filler_cancelled")
            print(
                f"[voice_agent] turn_skipped_noise reason=oov_english_token"
                f" text={_text_stripped!r} conf={stt_result.confidence:.3f}"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        if stt_result.confidence < _MIN_CONFIDENCE:
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler_decision", turn_type="noise",
                  reason="low_confidence", msg="filler_cancelled",
                  conf=f"{stt_result.confidence:.3f}")
            print(
                f"[voice_agent] turn_skipped_noise reason=low_confidence"
                f" text={_text_stripped!r} conf={stt_result.confidence:.3f}"
                f" session={self.ctx.session_id} turn={self._turn_index}",
                file=sys.stderr,
            )
            filler_task.cancel()
            return _skip
        # ── /Noise gate ───────────────────────────────────────────────────────

        # Mark that a real utterance has passed the noise gate this call.
        # From this point on, filler is eligible for this and all future turns.
        # Must be set AFTER all noise-gate checks so blips never unlock filler.
        self._real_utterance_seen = True

        # Unlock filler for THIS turn: signal that the noise gate accepted the
        # transcript so _play_filler may proceed to play (if gap timer already
        # fired) or will know to play (if it fires shortly after this).
        _noise_gate_event.set()
        _diag(self.ctx.session_id, self._turn_index,
              phase="filler_decision", turn_type="real",
              msg="noise_gate_passed_filler_unlocked")

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
            _diag(self.ctx.session_id, self._turn_index,
                  phase="filler_decision", turn_type="backchannel",
                  reason="barge_in_backchannel", msg="filler_cancelled")
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
        _milestone("playback_start", session=self.ctx.session_id,
                   turn=self._turn_index)
        brain: BrainOutput | None = None
        # Per-turn TTS accumulator: [total_chars, total_audio_bytes, sentence_count]
        self._tts_diag: list[int] = [0, 0, 0]

        try:
            brain = await self._run_streaming_llm_tts(
                stt_result, send_audio, t_stt, _first_audio_event
            )
        except Exception:  # noqa: BLE001
            self._playing_tts = False
            _milestone("playback_end", session=self.ctx.session_id,
                       turn=self._turn_index, reason="exception")
            logger.exception(
                "Streaming LLM+TTS failed for session=%s turn=%s — skipping turn",
                self.ctx.session_id, self._turn_index,
            )
            return _skip
        finally:
            if self._playing_tts:
                _milestone("playback_end", session=self.ctx.session_id,
                           turn=self._turn_index, reason="complete")
            self._playing_tts = False

        if brain is None:
            return _skip

        t_done = time.time()
        _turn_total_ms = (t_done - t0) * 1000
        _milestone("reply_done", session=self.ctx.session_id, turn=self._turn_index,
                   total_ms=f"{_turn_total_ms:.0f}")
        logger.info(
            "latency session=%s turn=%s leg=total ms=%.0f",
            self.ctx.session_id, self._turn_index, _turn_total_ms,
        )
        # ── [diag] LLM summary ────────────────────────────────────────────────
        _diag(self.ctx.session_id, self._turn_index,
              phase="llm",
              user_turn=repr((stt_result.text or "")[:60]),
              history_turns=len(self._dialog_history),
              slots_collected=len(getattr(self.ctx, "__dict__", {})),
              reply=repr((brain.reply or "")[:80]),
              next_action=brain.next_action,
              total_ms=f"{_turn_total_ms:.0f}")
        # ── [diag] TTS summary ────────────────────────────────────────────────
        _tts_chars, _tts_bytes, _tts_sentences = getattr(self, "_tts_diag", [0, 0, 0])
        _diag(self.ctx.session_id, self._turn_index,
              phase="tts",
              model=os.getenv("SARVAM_TTS_MODEL", "bulbul:v3"),
              speaker=os.getenv("SARVAM_TTS_SPEAKER", "priya"),
              sentences=_tts_sentences,
              chars=_tts_chars,
              audio_bytes=_tts_bytes)
        # ── [diag] spoken text ────────────────────────────────────────────────
        _diag(self.ctx.session_id, self._turn_index,
              phase="spoken",
              text=repr((brain.reply or "")[:120]))

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
        _t_first_sentence_queued: float | None = None  # when first sentence hit tts_queue

        # ── Start parallel metadata call (non-blocking) ────────────────────────
        _slots_snapshot = dict(self._accumulated_slots) if self._accumulated_slots else None
        metadata_task: asyncio.Task = asyncio.create_task(
            self._llm.generate(self.ctx, stt_result.text, self._dialog_history,
                               collected_slots=_slots_snapshot)
        )

        # ── TTS worker (same sentence-queue pattern as _stream_path) ──────────
        tts_queue: asyncio.Queue[tuple[str, bool]] = asyncio.Queue()

        async def _tts_worker():
            nonlocal first_audio_sent, _t_first_sentence_queued
            while True:
                sentence, is_done = await tts_queue.get()
                if is_done and not sentence:
                    break
                if not sentence:
                    continue
                if self._stop_playback.is_set():
                    break
                try:
                    _t_synth_start = time.time()
                    # Flag-gated streaming path (first-audio ~0.3s). Streams chunks
                    # to send_audio as they arrive. Returns False → use batch below.
                    streamed = await self._synth_and_play_stream(sentence, send_audio)
                    if not streamed and not self._stop_playback.is_set():
                        tts_result = await self._tts.synthesize(
                            sentence,
                            self.ctx.lang,
                            self.ctx.voice_profile_id,
                            self.ctx.tenant_id,
                            self.ctx.session_id,
                            self.ctx.tts_premium,
                        )
                        # Accumulate TTS diag stats
                        if hasattr(self, "_tts_diag"):
                            self._tts_diag[0] += len(sentence)
                            self._tts_diag[1] += len(tts_result.audio)
                            self._tts_diag[2] += 1
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
                            # [diag] first_audio breakdown: leg timings for bottleneck analysis
                            _q_ms = (_t_synth_start - (_t_first_sentence_queued or _t_synth_start)) * 1000
                            _synth_ms = (t_audio - _t_synth_start) * 1000
                            _total_ms = (t_audio - t_stt) * 1000
                            _diag(self.ctx.session_id, self._turn_index,
                                  phase="first_audio",
                                  mode="stream" if streamed else "batch",
                                  total_ms=f"{_total_ms:.0f}",
                                  llm_to_sentence_ms=f"{_q_ms:.0f}",
                                  tts_synth_ms=f"{_synth_ms:.0f}",
                                  sentence_chars=len(sentence))
                        if not streamed:
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
            _chunk_is_first = True  # breath-rhythm: first chunk uses lower threshold
            async for token, _ in self._llm.generate_stream_text(
                self.ctx, stt_result.text, self._dialog_history,
                collected_slots=_slots_snapshot,
            ):
                if not first_token_logged and token:
                    first_token_logged = True
                    _t_first_token_ms = (time.time() - t_stt) * 1000
                    _milestone(
                        "llm_first_token",
                        session=self.ctx.session_id,
                        turn=self._turn_index,
                        ms=f"{_t_first_token_ms:.0f}",
                    )
                    _diag(self.ctx.session_id, self._turn_index,
                          phase="llm_first_token",
                          path="stream_text",
                          history_turns=len(self._dialog_history),
                          first_token_ms=f"{_t_first_token_ms:.0f}")

                if token:
                    token_buffer += token
                    full_spoken += token

                    if _should_flush(token_buffer, is_first_chunk=_chunk_is_first):
                        sentence = token_buffer.strip()
                        token_buffer = ""
                        if sentence:
                            if _t_first_sentence_queued is None:
                                _t_first_sentence_queued = time.time()
                            _diag(self.ctx.session_id, self._turn_index,
                                  phase="chunk",
                                  words=_word_count(sentence),
                                  first=_chunk_is_first)
                            _chunk_is_first = False
                            await tts_queue.put((sentence, False))

                # Barge-in during LLM streaming: record partial and abort
                if self._stop_playback.is_set():
                    self._interrupted_partial = full_spoken.strip() or None
                    break

            # Flush any remaining buffer (llm_done=True → always flush)
            if not self._stop_playback.is_set() and token_buffer.strip():
                sentence = token_buffer.strip()
                if _t_first_sentence_queued is None:
                    _t_first_sentence_queued = time.time()
                _diag(self.ctx.session_id, self._turn_index,
                      phase="chunk",
                      words=_word_count(sentence),
                      first=_chunk_is_first,
                      llm_done=True)
                await tts_queue.put((sentence, False))
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
        # ── Update accumulated slots from this turn's metadata ─────────────────
        if meta_brain is not None:
            try:
                _update_accumulated_slots(self._accumulated_slots, meta_brain)
            except Exception:
                logger.debug("_update_accumulated_slots failed — continuing", exc_info=True)
        _diag(self.ctx.session_id, self._turn_index,
              phase="slots",
              collected_slots=repr(self._accumulated_slots))
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
                    # Flag-gated streaming path (first-audio ~0.3s); False → batch.
                    streamed = await self._synth_and_play_stream(sentence, send_audio)
                    if not streamed and not self._stop_playback.is_set():
                        tts_result = await self._tts.synthesize(
                            sentence,
                            self.ctx.lang,
                            self.ctx.voice_profile_id,
                            self.ctx.tenant_id,
                            self.ctx.session_id,
                            self.ctx.tts_premium,
                        )
                        # Accumulate TTS diag stats
                        if hasattr(self, "_tts_diag"):
                            self._tts_diag[0] += len(sentence)
                            self._tts_diag[1] += len(tts_result.audio)
                            self._tts_diag[2] += 1
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
                        if not streamed:
                            await _call(send_audio, tts_result.audio)
                except Exception:  # noqa: BLE001
                    logger.exception(
                        "TTS failed for sentence=%r session=%s",
                        sentence[:40], self.ctx.session_id,
                    )

        tts_task = asyncio.create_task(_tts_worker())
        _chunk_is_first = True  # breath-rhythm: first chunk uses lower word threshold

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

                    # Flush any remaining buffer (llm_done=True)
                    if token_buffer.strip():
                        sentence = token_buffer.strip()
                        _diag(self.ctx.session_id, self._turn_index,
                              phase="chunk",
                              words=_word_count(sentence),
                              first=_chunk_is_first,
                              llm_done=True)
                        await tts_queue.put((sentence, False))
                    # Signal TTS worker to finish
                    await tts_queue.put(("", True))
                    break

                # Accumulate token
                if token:
                    token_buffer += token
                    full_reply += token

                    if _should_flush(token_buffer, is_first_chunk=_chunk_is_first):
                        sentence = token_buffer.strip()
                        token_buffer = ""
                        if sentence:
                            _diag(self.ctx.session_id, self._turn_index,
                                  phase="chunk",
                                  words=_word_count(sentence),
                                  first=_chunk_is_first)
                            _chunk_is_first = False
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
                # Accumulate TTS diag stats
                if hasattr(self, "_tts_diag"):
                    self._tts_diag[0] += len(sentence)
                    self._tts_diag[1] += len(tts_result.audio)
                    self._tts_diag[2] += 1
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
