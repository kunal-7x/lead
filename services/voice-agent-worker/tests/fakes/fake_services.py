from __future__ import annotations

import struct
from typing import AsyncIterator
from voice_agent.models import STTResult, BrainOutput, TTSResult, SessionContext

_SILENT_PCM = b"\x00\x00" * 400  # 50ms silence at 8kHz


class FakeSTT:
    def __init__(self, transcript: str = "2BHK ka price kya hai",
                 confidence: float = 0.90, engine: str = "sarvam") -> None:
        self.transcript = transcript
        self.confidence = confidence
        self.engine = engine
        self.call_count = 0

    async def transcribe(self, audio: bytes, lang: str, session_id: str) -> STTResult:
        self.call_count += 1
        return STTResult(text=self.transcript, confidence=self.confidence,
                         engine_used=self.engine, is_final=True)


class FakeLLM:
    def __init__(self, reply: str = "2BHK 60 lakh mein available hai.",
                 next_action: str = "qualify", lead_status: str = "warm",
                 handover: bool = False, site_visit: bool = False,
                 callback: bool = False, whatsapp: bool = False) -> None:
        self.reply = reply
        self.next_action = next_action
        self.lead_status = lead_status
        self.handover = handover
        self.site_visit = site_visit
        self.callback = callback
        self.whatsapp = whatsapp
        self.call_count = 0
        self.last_collected_slots: dict | None = None

    def _make_brain(self) -> BrainOutput:
        return BrainOutput(
            reply=self.reply,
            lead_status=self.lead_status,
            lead_score=65,
            next_action=self.next_action,
            should_handover_to_human=self.handover,
            should_create_site_visit=self.site_visit,
            should_create_callback=self.callback,
            should_send_whatsapp=self.whatsapp,
            risk_level="safe",
            confidence=0.88,
            summary="Warm lead interested in 2BHK",
        )

    async def generate(self, ctx: SessionContext, user_turn: str,
                       dialog_history: list,
                       collected_slots: dict | None = None) -> BrainOutput:
        self.call_count += 1
        self.last_collected_slots = collected_slots
        return self._make_brain()

    async def generate_stream(
        self, ctx: SessionContext, user_turn: str, dialog_history: list
    ) -> AsyncIterator[tuple[str, BrainOutput | None]]:
        """Fake streaming LLM: yields reply word-by-word then final brain."""
        self.call_count += 1
        brain = self._make_brain()
        words = self.reply.split()
        for i, word in enumerate(words):
            token = word + (" " if i < len(words) - 1 else "")
            yield (token, None)
        yield ("", brain)

    async def generate_stream_text(
        self, ctx: SessionContext, user_turn: str, dialog_history: list,
        collected_slots: dict | None = None,
    ) -> AsyncIterator[tuple[str, None]]:
        """Fake plain-text streaming LLM: yields reply word-by-word, no final brain."""
        # NOTE: generate() is called separately (parallel metadata) — don't increment
        # call_count here since the agent also calls generate() concurrently.
        brain = self._make_brain()
        words = brain.reply.split()
        for i, word in enumerate(words):
            token = word + (" " if i < len(words) - 1 else "")
            yield (token, None)


class FakeGuardrail:
    """Pass-through guardrail for tests."""
    async def check(self, brain: BrainOutput, kb_chunks: list,
                    user_turn: str) -> BrainOutput:
        return brain


class FakeTTS:
    def __init__(self, audio: bytes | None = None,
                 tier: str = "sarvam_bulbul",
                 stream_chunks: list[bytes] | None = None,
                 stream_raises: bool = False) -> None:
        self._audio = audio or _SILENT_PCM
        self._tier = tier
        self.call_count = 0
        self.stream_call_count = 0
        # If set, synthesize_stream yields these PCM16 8kHz chunks.
        self._stream_chunks = stream_chunks
        self._stream_raises = stream_raises

    async def synthesize(self, text: str, lang: str, voice_id: str,
                         tenant_id: str, session_id: str,
                         tts_premium: bool = False) -> TTSResult:
        self.call_count += 1
        return TTSResult(audio=self._audio, tier_used=self._tier, cache_hit=False)

    async def synthesize_stream(self, text: str, lang: str, voice_id: str):
        self.stream_call_count += 1
        if self._stream_raises:
            raise RuntimeError("streaming_disabled")
        chunks = self._stream_chunks if self._stream_chunks is not None else [_SILENT_PCM]
        for c in chunks:
            yield c


class FakeFreeSwitchWS:
    """Simulates FreeSWITCH sending audio frames over WebSocket."""

    def __init__(self, n_speech_chunks: int = 10, n_silence_chunks: int = 40) -> None:
        self.n_speech_chunks = n_speech_chunks
        self.n_silence_chunks = n_silence_chunks
        self._sent: list[bytes] = []
        self._received: list[dict] = []

    def audio_chunks(self, speech: bool = True) -> list[bytes]:
        # 320 bytes = 20ms at 8kHz L16
        if speech:
            sample = struct.pack("<160h", *([1000] * 160))  # non-zero = speech
        else:
            sample = b"\x00\x00" * 160                     # zero = silence
        return [sample] * (self.n_speech_chunks if speech else self.n_silence_chunks)

    def all_chunks(self) -> list[bytes]:
        return self.audio_chunks(True) + self.audio_chunks(False)
