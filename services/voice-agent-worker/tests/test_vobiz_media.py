"""tests/test_vobiz_media.py — Unit tests for Vobiz codec and media bridge.

Covers:
  1. Codec round-trip: µ-law → PCM16 → µ-law (lossy, so check correlation)
  2. Codec round-trip: PCM16 → µ-law → PCM16 (same check)
  3. Full media-bridge integration using fake services and a fake Vobiz WS.
     Verifies: STT/LLM/TTS invoked, outbound µ-law frame sent, turn recorded,
     completes within latency budget.
"""
from __future__ import annotations

import asyncio
import base64
import json
import struct
import time
from dataclasses import dataclass, field
from typing import AsyncIterator

import numpy as np
import pytest

from voice_agent.vobiz_codec import ulaw_to_pcm16, pcm16_to_ulaw
from voice_agent.vobiz_media import run_vobiz_bridge
from voice_agent.models import SessionContext
from voice_agent.actions import FakePublisher
from voice_agent.recorder import FakeTurnStore
from voice_agent.vad import FakeVAD
from tests.fakes.fake_services import FakeSTT, FakeLLM, FakeGuardrail, FakeTTS


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _pcm_speech_chunk() -> bytes:
    """320-byte (20ms @ 8kHz) PCM16 LE speech chunk with non-zero amplitude."""
    return struct.pack("<160h", *([1000] * 160))


def _pcm_silence_chunk() -> bytes:
    """320-byte silence chunk."""
    return b"\x00\x00" * 160


def _pcm_to_vobiz_media_frame(pcm_chunk: bytes) -> str:
    """Encode a PCM chunk as a Vobiz inbound 'media' JSON frame (µ-law payload)."""
    ulaw = pcm16_to_ulaw(pcm_chunk)
    payload = base64.b64encode(ulaw).decode()
    return json.dumps({"event": "media", "media": {"payload": payload}})


# ---------------------------------------------------------------------------
# 1. Codec tests
# ---------------------------------------------------------------------------

class TestVobizCodec:
    def test_ulaw_to_pcm16_length(self):
        """Output is 2× input length."""
        ulaw = bytes(range(256))
        pcm = ulaw_to_pcm16(ulaw)
        assert len(pcm) == len(ulaw) * 2

    def test_pcm16_to_ulaw_length(self):
        """Output is ½ input length."""
        pcm = _pcm_speech_chunk()
        ulaw = pcm16_to_ulaw(pcm)
        assert len(ulaw) == len(pcm) // 2

    def test_ulaw_roundtrip_idempotency(self):
        """µ-law → PCM16 → µ-law: re-encoded bytes are nearly identical.

        G.711 has one special case: both 0x7F (negative-zero) and 0xFF
        (positive-zero) decode to PCM 0, but PCM 0 re-encodes to canonical
        0xFF. This is correct ITU-T G.711 behavior — exactly 1 byte out of
        256 possible µ-law values (0x7F) maps to a different canonical value
        on re-encode. We allow at most 1 such remapping.
        """
        ulaw_in = bytes(range(256))
        pcm = ulaw_to_pcm16(ulaw_in)
        ulaw_out = pcm16_to_ulaw(pcm)
        # Count mismatches — at most 1 is acceptable (the 0x7F→0xFF canonical remap)
        mismatches = sum(a != b for a, b in zip(ulaw_in, ulaw_out))
        assert mismatches <= 1, (
            f"Expected at most 1 canonical-remap mismatch, got {mismatches}"
        )

    def test_pcm16_roundtrip_closeness(self):
        """PCM16 → µ-law → PCM16: decoded signal correlates strongly with original.

        µ-law uses 8-bit companded quantisation of 16-bit linear audio.
        The high dynamic range of G.711 means large-amplitude signals
        (common in speech) are preserved with Pearson correlation > 0.9999.
        """
        rng = np.random.default_rng(42)
        # Speech-range amplitudes: ±8192 (quarter scale), scaled to use full range
        orig = (rng.integers(-8192, 8192, 480) * 4).astype(np.int16)
        pcm_bytes = orig.astype("<i2").tobytes()

        ulaw = pcm16_to_ulaw(pcm_bytes)
        recovered_bytes = ulaw_to_pcm16(ulaw)
        recovered = np.frombuffer(recovered_bytes, dtype="<i2").astype(np.float32)
        orig_f = orig.astype(np.float32)

        # Pearson correlation — µ-law G.711 achieves very high correlation
        # for mid-to-large amplitude signals (its companding favors small signals)
        corr = np.corrcoef(orig_f, recovered)[0, 1]
        assert corr > 0.999, f"Round-trip correlation {corr:.4f} below 0.999"

    def test_silence_encodes_to_constant(self):
        """PCM silence (all zeros) encodes to a single µ-law byte value."""
        silence_pcm = b"\x00\x00" * 160
        ulaw = pcm16_to_ulaw(silence_pcm)
        unique = set(ulaw)
        assert len(unique) == 1, f"Silence should map to one µ-law value, got {unique}"

    def test_ulaw_decode_produces_nonzero_for_nonzero_input(self):
        """Non-zero µ-law bytes decode to non-zero PCM samples."""
        ulaw = bytes([0x00, 0x7F, 0x80, 0xFF])  # various µ-law byte values
        pcm = ulaw_to_pcm16(ulaw)
        samples = struct.unpack("<4h", pcm)
        # At least some samples should be non-zero (0x7F and 0x80 are large values)
        assert any(s != 0 for s in samples)


# ---------------------------------------------------------------------------
# 2. Fake WebSocket for bridge testing
# ---------------------------------------------------------------------------

@dataclass
class FakeVobizWS:
    """Simulates the Vobiz WebSocket from the platform side.

    Constructor args:
        n_speech_chunks: PCM speech chunks to emit (will be µ-law encoded)
        n_silence_chunks: PCM silence chunks to emit after speech
    """
    n_speech_chunks: int = 10
    n_silence_chunks: int = 40
    call_id: str = "test-call-001"
    stream_id: str = "stream-001"

    sent_frames: list[str] = field(default_factory=list)   # outbound frames captured
    _frames: list[str] = field(default_factory=list)       # inbound frames to serve
    _idx: int = 0

    def __post_init__(self):
        frames = []
        # "start" frame
        frames.append(json.dumps({
            "event": "start",
            "start": {
                "streamId": self.stream_id,
                "callId": self.call_id,
            }
        }))
        # speech media frames
        for _ in range(self.n_speech_chunks):
            frames.append(_pcm_to_vobiz_media_frame(_pcm_speech_chunk()))
        # silence media frames (enough to trigger VAD end-of-utterance)
        for _ in range(self.n_silence_chunks):
            frames.append(_pcm_to_vobiz_media_frame(_pcm_silence_chunk()))
        # "stop" frame
        frames.append(json.dumps({"event": "stop"}))
        self._frames = frames

    async def receive_text(self) -> str:
        """Serve the next inbound frame; simulate a tiny async delay."""
        if self._idx >= len(self._frames):
            # Hang until disconnected (AgentLoop has already finished)
            await asyncio.sleep(9999)
        frame = self._frames[self._idx]
        self._idx += 1
        await asyncio.sleep(0)  # yield control
        return frame

    async def send_text(self, text: str) -> None:
        """Capture outbound frames."""
        self.sent_frames.append(text)


# ---------------------------------------------------------------------------
# 3. Integration test: full bridge run
# ---------------------------------------------------------------------------

async def _run_bridge(ws: FakeVobizWS, ctx: SessionContext,
                      stt: FakeSTT, llm: FakeLLM, guardrail: FakeGuardrail,
                      tts: FakeTTS, publisher: FakePublisher,
                      store: FakeTurnStore, vad: FakeVAD) -> None:
    """Run the bridge with all-fake services injected."""

    async def fake_load_ctx(call_id: str) -> SessionContext:
        return ctx

    def make_services():
        return stt, llm, guardrail, tts, publisher, store, vad

    await run_vobiz_bridge(
        ws,
        ws.call_id,
        _load_context_fn=fake_load_ctx,
        _make_services_fn=make_services,
    )


class TestVobizMediaBridge:
    """Integration tests for the Vobiz ↔ AgentLoop bridge."""

    @pytest.fixture
    def ctx(self):
        return SessionContext(
            session_id="test-call-001",
            tenant_id="tenant-test",
            campaign_id="camp-test",
            lang="hi-en",
            voice_profile_id="meera",
            project_id="proj-test",
        )

    @pytest.fixture
    def fake_stt(self):
        return FakeSTT(transcript="2BHK ka price kya hai", confidence=0.90)

    @pytest.fixture
    def fake_llm(self):
        return FakeLLM(reply="2BHK 60 lakh mein available hai.", next_action="qualify")

    @pytest.fixture
    def fake_guardrail(self):
        return FakeGuardrail()

    @pytest.fixture
    def fake_tts(self):
        # TTS returns PCM silence (what it would normally produce)
        return FakeTTS(audio=b"\x00\x00" * 400)

    @pytest.fixture
    def fake_publisher(self):
        return FakePublisher()

    @pytest.fixture
    def fake_store(self):
        return FakeTurnStore()

    @pytest.fixture
    def fake_vad(self):
        # Speech for first 10 chunks, then silence
        return FakeVAD(speech_chunks=10)

    async def test_stt_llm_tts_invoked(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """STT, LLM, and TTS are each called at least once during a normal turn."""
        ws = FakeVobizWS(n_speech_chunks=10, n_silence_chunks=40)
        await _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                          fake_tts, fake_publisher, fake_store, fake_vad)

        assert fake_stt.call_count >= 1, "STT should have been called"
        assert fake_llm.call_count >= 1, "LLM should have been called"
        assert fake_tts.call_count >= 1, "TTS should have been called"

    async def test_outbound_ulaw_frame_sent(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """At least one outbound µ-law media frame with non-empty payload is sent."""
        ws = FakeVobizWS(n_speech_chunks=10, n_silence_chunks=40)
        await _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                          fake_tts, fake_publisher, fake_store, fake_vad)

        media_frames = [
            json.loads(f) for f in ws.sent_frames
            if json.loads(f).get("event") == "playAudio"
        ]
        assert len(media_frames) >= 1, (
            f"Expected at least one 'playAudio' frame; got frames: "
            f"{[json.loads(f).get('event') for f in ws.sent_frames]}"
        )

        # Payload must be non-empty base64-decodable µ-law bytes
        first = media_frames[0]
        payload_b64 = first["media"]["payload"]
        ulaw_bytes = base64.b64decode(payload_b64)
        assert len(ulaw_bytes) > 0, "µ-law payload must be non-empty"

    async def test_turn_recorded(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """A turn is saved to the turn store after the utterance is processed."""
        ws = FakeVobizWS(n_speech_chunks=10, n_silence_chunks=40)
        await _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                          fake_tts, fake_publisher, fake_store, fake_vad)

        assert len(fake_store.turns) >= 1, "At least one turn should be recorded"
        turn = fake_store.turns[0]
        assert turn.session_id == ctx.session_id
        assert turn.transcript == fake_stt.transcript

    async def test_call_completed_in_store(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """complete_call() is called on the store after the loop finishes."""
        ws = FakeVobizWS(n_speech_chunks=10, n_silence_chunks=40)
        await _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                          fake_tts, fake_publisher, fake_store, fake_vad)

        assert len(fake_store.completed) >= 1, "complete_call should have been called"

    async def test_latency_budget(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """With all-fake services the entire bridge completes within 1 second."""
        ws = FakeVobizWS(n_speech_chunks=10, n_silence_chunks=40)
        t0 = time.perf_counter()
        await _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                          fake_tts, fake_publisher, fake_store, fake_vad)
        elapsed_ms = (time.perf_counter() - t0) * 1000
        assert elapsed_ms < 1000, f"Bridge took {elapsed_ms:.1f}ms (expected <1000ms)"

    async def test_outbound_frame_structure(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """Outbound media frames have the expected Plivo-style shape."""
        ws = FakeVobizWS(n_speech_chunks=10, n_silence_chunks=40)
        await _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                          fake_tts, fake_publisher, fake_store, fake_vad)

        media_frames = [
            json.loads(f) for f in ws.sent_frames
            if json.loads(f).get("event") == "playAudio"
        ]
        assert media_frames, "Should have at least one playAudio frame"
        frame = media_frames[0]
        assert frame["event"] == "playAudio"
        assert "media" in frame
        assert frame["media"]["contentType"] == "audio/x-mulaw"
        assert frame["media"]["sampleRate"] == 8000
        assert isinstance(frame["media"]["payload"], str)

    async def test_stop_event_ends_bridge(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """A 'stop' frame causes the bridge to exit cleanly."""
        # Very short: just start + stop, no audio — should complete quickly
        @dataclass
        class QuickStopWS:
            call_id: str = "quick-stop"
            stream_id: str = "stream-quick"
            sent_frames: list = field(default_factory=list)
            _frames: list = field(default_factory=lambda: [
                json.dumps({"event": "start",
                            "start": {"streamId": "stream-quick",
                                      "callId": "quick-stop"}}),
                json.dumps({"event": "stop"}),
            ])
            _idx: int = 0

            async def receive_text(self):
                if self._idx >= len(self._frames):
                    await asyncio.sleep(9999)
                frame = self._frames[self._idx]
                self._idx += 1
                return frame

            async def send_text(self, text):
                self.sent_frames.append(text)

        ws = QuickStopWS()
        # Should complete without hanging
        await asyncio.wait_for(
            _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                        fake_tts, fake_publisher, fake_store, fake_vad),
            timeout=2.0,
        )

    async def test_no_start_frame_bridge_exits(
        self, ctx, fake_stt, fake_llm, fake_guardrail,
        fake_tts, fake_publisher, fake_store, fake_vad
    ):
        """If WS closes before 'start' frame, bridge exits without error."""
        @dataclass
        class NoStartWS:
            call_id: str = "no-start"
            sent_frames: list = field(default_factory=list)

            async def receive_text(self):
                # Immediately raise disconnect-style error
                raise ConnectionResetError("WS closed")

            async def send_text(self, text):
                self.sent_frames.append(text)

        ws = NoStartWS()
        # Should complete without error and within timeout
        await asyncio.wait_for(
            _run_bridge(ws, ctx, fake_stt, fake_llm, fake_guardrail,
                        fake_tts, fake_publisher, fake_store, fake_vad),
            timeout=2.0,
        )
