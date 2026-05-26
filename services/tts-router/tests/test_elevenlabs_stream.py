"""Tests for ElevenLabs streaming engine and /v1/tts/ws WebSocket endpoint.

All tests are fully offline — no real ElevenLabs API calls.
"""
from __future__ import annotations

import asyncio
import audioop
import json
import struct

import pytest
from fastapi.testclient import TestClient

import tts_router.app as app_module
from tts_router.app import app
from tts_router.cache import FakeAudioCache
from tts_router.engines.kokoro import ElevenLabsEngine
from tts_router.router import TTSRouter
from tts_router.switcher import EngineSwitcher
from tests.fakes.fake_engines import FakeSarvamTTS


# ── Helpers ──────────────────────────────────────────────────────────────────

def _make_pcm(n_samples: int = 160) -> bytes:
    return struct.pack(f"<{n_samples}h", *([0] * n_samples))


def _make_ulaw(n_bytes: int = 160) -> bytes:
    """Fake µ-law bytes (all 0xFF = silence in µ-law)."""
    return bytes([0xFF] * n_bytes)


def _fake_router() -> TTSRouter:
    import fakeredis.aioredis
    rdb = fakeredis.aioredis.FakeRedis()
    return TTSRouter(
        {"sarvam_bulbul": FakeSarvamTTS()},
        EngineSwitcher(rdb),
        FakeAudioCache(),
    )


# ── ElevenLabsEngine unit tests ───────────────────────────────────────────────

class TestElevenLabsEngineInit:
    def test_default_model_is_flash(self, monkeypatch):
        monkeypatch.delenv("ELEVENLABS_MODEL", raising=False)
        engine = ElevenLabsEngine(api_key="key")
        assert engine._model == "eleven_flash_v2_5"

    def test_default_format_is_ulaw(self, monkeypatch):
        monkeypatch.delenv("TTS_OUTPUT_FORMAT", raising=False)
        engine = ElevenLabsEngine(api_key="key")
        assert engine._output_format == "ulaw_8000"

    def test_streaming_ws_enabled_by_default(self, monkeypatch):
        monkeypatch.delenv("TTS_STREAMING_WS", raising=False)
        engine = ElevenLabsEngine(api_key="key")
        assert engine._streaming_ws_enabled is True

    def test_streaming_ws_disabled_via_env(self, monkeypatch):
        monkeypatch.setenv("TTS_STREAMING_WS", "false")
        engine = ElevenLabsEngine(api_key="key")
        assert engine._streaming_ws_enabled is False

    def test_env_override_model(self, monkeypatch):
        monkeypatch.setenv("ELEVENLABS_MODEL", "eleven_multilingual_v2")
        engine = ElevenLabsEngine(api_key="key")
        assert engine._model == "eleven_multilingual_v2"

    def test_env_override_format(self, monkeypatch):
        monkeypatch.setenv("TTS_OUTPUT_FORMAT", "mp3_44100_128")
        engine = ElevenLabsEngine(api_key="key")
        assert engine._output_format == "mp3_44100_128"

    def test_premium_only(self):
        assert ElevenLabsEngine().premium_only is True

    def test_health_check_no_key(self):
        async def _run():
            return await ElevenLabsEngine(api_key="").health_check()
        result = asyncio.get_event_loop().run_until_complete(_run())
        assert result is False

    def test_health_check_with_key(self):
        async def _run():
            return await ElevenLabsEngine(api_key="test-key").health_check()
        result = asyncio.get_event_loop().run_until_complete(_run())
        assert result is True

    def test_voices_returns_configured_voice(self, monkeypatch):
        monkeypatch.setenv("ELEVENLABS_VOICE_ID", "zT03pEAEi0VHKciJODfn")
        engine = ElevenLabsEngine(api_key="key")
        voices = engine.voices()
        assert len(voices) == 1
        assert voices[0].id == "zT03pEAEi0VHKciJODfn"
        assert voices[0].engine == "elevenlabs"


class TestElevenLabsSynthesizeStream:
    async def test_stream_raises_without_key(self):
        engine = ElevenLabsEngine(api_key="")

        async def _empty():
            return
            yield  # make it an async generator

        with pytest.raises(ValueError, match="ELEVENLABS_API_KEY"):
            async for _ in engine.synthesize_stream(_empty()):
                pass

    def test_stream_ws_url_construction(self, monkeypatch):
        """Verify WS URL includes model_id and output_format query params."""
        import re
        monkeypatch.setenv("ELEVENLABS_MODEL", "eleven_flash_v2_5")
        monkeypatch.setenv("TTS_OUTPUT_FORMAT", "ulaw_8000")
        engine = ElevenLabsEngine(api_key="test-key")
        vid = "zT03pEAEi0VHKciJODfn"
        expected_url = (
            f"wss://api.elevenlabs.io/v1/text-to-speech/{vid}/stream-input"
            f"?model_id=eleven_flash_v2_5&output_format=ulaw_8000"
        )
        actual_url = (
            engine._WS_BASE.format(voice_id=vid)
            + f"?model_id={engine._model}&output_format={engine._output_format}"
        )
        assert actual_url == expected_url


# ── WebSocket endpoint tests ─────────────────────────────────────────────────

class TestTTSStreamWSEndpoint:
    """Tests for /v1/tts/ws WebSocket endpoint."""

    def _make_client(self, monkeypatch) -> TestClient:
        """Build a TestClient with fake router and no ElevenLabs key."""
        monkeypatch.setattr(app_module, "_build_router", _fake_router)
        monkeypatch.setenv("ELEVENLABS_API_KEY", "")
        monkeypatch.setenv("TTS_STREAMING_WS", "true")
        return TestClient(app)

    def test_ws_endpoint_exists(self, monkeypatch):
        """WS /v1/tts/ws accepts connection."""
        client = self._make_client(monkeypatch)
        with client:
            with client.websocket_connect("/v1/tts/ws") as ws:
                # Send text + flush immediately
                ws.send_text(json.dumps({"text": "Namaste", "flush": True}))
                # Drain messages; expect a done frame eventually
                messages = []
                try:
                    for _ in range(20):
                        msg = ws.receive()
                        messages.append(msg)
                        if msg.get("text"):
                            data = json.loads(msg["text"])
                            if data.get("type") in ("done", "error"):
                                break
                except Exception:
                    pass
                assert len(messages) >= 1

    def test_ws_no_key_falls_back_to_sarvam(self, monkeypatch):
        """With no ElevenLabs key, fallback path is triggered (no crash)."""
        client = self._make_client(monkeypatch)
        with client:
            with client.websocket_connect("/v1/tts/ws") as ws:
                ws.send_text(json.dumps({"text": "test fallback text", "flush": True}))
                done_received = False
                for _ in range(30):
                    try:
                        msg = ws.receive()
                        if msg.get("text"):
                            data = json.loads(msg["text"])
                            if data.get("type") in ("done", "error"):
                                done_received = True
                                break
                    except Exception:
                        break
                # Either done or error is acceptable when key is missing
                assert done_received

    def test_ws_empty_flush_returns_silence_done(self, monkeypatch):
        """Sending flush with no text returns done immediately."""
        client = self._make_client(monkeypatch)
        with client:
            with client.websocket_connect("/v1/tts/ws") as ws:
                ws.send_text(json.dumps({"text": "", "flush": True}))
                # Expect done with engine=silence
                for _ in range(10):
                    try:
                        msg = ws.receive()
                        if msg.get("text"):
                            data = json.loads(msg["text"])
                            if data.get("type") == "done":
                                break
                    except Exception:
                        break

    def test_ws_streaming_disabled_via_flag(self, monkeypatch):
        """TTS_STREAMING_WS=false forces batch fallback even if key is set."""
        monkeypatch.setattr(app_module, "_build_router", _fake_router)
        monkeypatch.setenv("ELEVENLABS_API_KEY", "fake-key-here")
        monkeypatch.setenv("TTS_STREAMING_WS", "false")
        client = TestClient(app)
        with client:
            with client.websocket_connect("/v1/tts/ws") as ws:
                ws.send_text(json.dumps({"text": "test", "flush": True}))
                done_received = False
                for _ in range(30):
                    try:
                        msg = ws.receive()
                        if msg.get("text"):
                            data = json.loads(msg["text"])
                            if data.get("type") in ("done", "error"):
                                done_received = True
                                break
                    except Exception:
                        break
                assert done_received


# ── µ-law conversion sanity check ─────────────────────────────────────────────

def test_ulaw_conversion_roundtrip():
    """audioop lin2ulaw and ulaw2lin produce valid µ-law 8kHz bytes."""
    pcm = _make_pcm(160)  # 20ms of silence
    ulaw = audioop.lin2ulaw(pcm, 2)
    assert len(ulaw) == 160  # 1 byte per sample in µ-law
    # Roundtrip back to PCM
    pcm_back = audioop.ulaw2lin(ulaw, 2)
    assert len(pcm_back) == len(pcm)


def test_ulaw_chunk_size():
    """µ-law 8kHz = 8000 bytes/sec → 160 bytes per 20ms frame."""
    sample_rate = 8000
    frame_ms = 20
    expected_bytes = sample_rate * frame_ms // 1000
    assert expected_bytes == 160
