from __future__ import annotations

import json
import pytest
import fakeredis.aioredis
from fastapi.testclient import TestClient

from stt_router.app import app
from stt_router.router import STTRouter
from stt_router.switcher import EngineSwitcher
from tests.fakes.fake_sarvam import FakeSarvamEngine
from tests.fakes.fake_indicconformer import FakeIndicConformerEngine
from tests.fakes.fake_sarvam_streaming import FakeSarvamStreamingEngine


def _make_test_router() -> STTRouter:
    import asyncio
    rdb = fakeredis.aioredis.FakeRedis()
    switcher = EngineSwitcher(rdb)
    engines = {
        "sarvam": FakeSarvamEngine(transcript="test transcript", confidence=0.90),
        "indicconformer": FakeIndicConformerEngine(),
    }
    return STTRouter(engines, switcher)


def _make_fake_streaming_engine():
    return FakeSarvamStreamingEngine(transcript="streaming test transcript", confidence=0.91)


@pytest.fixture
def client(monkeypatch):
    import stt_router.app as app_module
    # Patch _build_router so startup() installs our fake router, not real Redis
    monkeypatch.setattr(app_module, "_build_router", _make_test_router)
    # Enable streaming engine for tests: flag must be "sarvam" so startup() creates it
    monkeypatch.setattr(app_module, "_STT_STREAMING_ENGINE", "sarvam")
    # Patch SarvamStreamingEngine in app_module's namespace (it was imported there)
    _fake_engine = _make_fake_streaming_engine()
    monkeypatch.setattr(app_module, "SarvamStreamingEngine", lambda *a, **kw: _fake_engine)
    with TestClient(app) as c:
        yield c


@pytest.fixture
def client_streaming_disabled(monkeypatch):
    """Client fixture with STT_STREAMING_ENGINE=disabled (default production value)."""
    import stt_router.app as app_module
    monkeypatch.setattr(app_module, "_build_router", _make_test_router)
    monkeypatch.setattr(app_module, "_STT_STREAMING_ENGINE", "disabled")
    with TestClient(app) as c:
        yield c


def test_healthz(client):
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.json()["status"] == "ok"


def test_engine_health(client):
    resp = client.get("/v1/health/engines")
    assert resp.status_code == 200
    body = resp.json()
    assert "engines" in body
    assert len(body["engines"]) >= 1


def test_stt_batch(client):
    audio = b"\x00\x01" * 320
    resp = client.post(
        "/v1/stt/batch",
        files={"file": ("audio.raw", audio, "application/octet-stream")},
        data={"lang": "hi-en", "session_id": "test-sess", "tenant_id": "t1"},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["text"] == "test transcript"
    assert body["engine_used"] == "sarvam"
    assert body["is_final"] is True


def test_stt_realtime_stream_pcm16(client):
    """Realtime WS: send PCM16 frames + empty sentinel → get interim + final."""
    with client.websocket_connect(
        "/v1/stt/realtime?lang=hi-en&session_id=rt-test&audio_format=pcm16"
    ) as ws:
        # Send 500ms of silence audio (PCM16 8kHz = 8000 bytes/s → 4000 bytes = 500ms)
        pcm_chunk = b"\x00\x01" * 2000  # 4000 bytes PCM16
        ws.send_bytes(pcm_chunk)
        # Send empty sentinel to trigger transcription
        ws.send_bytes(b"")
        # Collect messages until final
        messages = []
        for _ in range(10):
            raw = ws.receive_text()
            msg = json.loads(raw)
            messages.append(msg)
            if msg.get("type") in ("final", "error"):
                break

    types = [m["type"] for m in messages]
    assert "final" in types, f"No final in messages: {messages}"
    final = next(m for m in messages if m["type"] == "final")
    assert final["text"] == "streaming test transcript"
    assert final["engine_used"] == "sarvam_streaming"


def test_stt_realtime_stream_ulaw(client):
    """Realtime WS: send µ-law frames (telephony format) → get final transcript."""
    with client.websocket_connect(
        "/v1/stt/realtime?lang=hi-en&session_id=rt-ulaw&audio_format=ulaw"
    ) as ws:
        # µ-law 8kHz: 8000 bytes/s → 4000 bytes = 500ms
        ulaw_chunk = b"\x7f" * 4000  # µ-law silence
        ws.send_bytes(ulaw_chunk)
        ws.send_bytes(b"")  # sentinel
        messages = []
        for _ in range(10):
            raw = ws.receive_text()
            msg = json.loads(raw)
            messages.append(msg)
            if msg.get("type") in ("final", "error"):
                break

    final = next((m for m in messages if m["type"] == "final"), None)
    assert final is not None, f"No final in: {messages}"
    assert final["text"] == "streaming test transcript"


def test_stt_realtime_flush_signal(client):
    """Realtime WS: text flush control message also triggers transcription."""
    with client.websocket_connect(
        "/v1/stt/realtime?lang=hi-en&session_id=rt-flush&audio_format=pcm16"
    ) as ws:
        ws.send_bytes(b"\x00\x01" * 2000)
        ws.send_text(json.dumps({"type": "flush"}))
        messages = []
        for _ in range(10):
            raw = ws.receive_text()
            msg = json.loads(raw)
            messages.append(msg)
            if msg.get("type") in ("final", "error"):
                break

    final = next((m for m in messages if m["type"] == "final"), None)
    assert final is not None, f"No final: {messages}"
    assert final["text"] == "streaming test transcript"


def test_stt_realtime_fallback_to_batch(client, monkeypatch):
    """When streaming engine is disabled, falls back to batch STT."""
    import stt_router.app as app_module
    # Disable streaming engine
    monkeypatch.setattr(app_module, "_streaming_engine", None)

    with client.websocket_connect(
        "/v1/stt/realtime?lang=hi-en&session_id=rt-fallback&audio_format=pcm16"
    ) as ws:
        ws.send_bytes(b"\x00\x01" * 2000)
        ws.send_bytes(b"")  # sentinel
        messages = []
        for _ in range(5):
            raw = ws.receive_text()
            msg = json.loads(raw)
            messages.append(msg)
            if msg.get("type") in ("final", "error"):
                break

    final = next((m for m in messages if m["type"] == "final"), None)
    assert final is not None
    # Batch engine returns "test transcript"
    assert final["text"] == "test transcript"
    assert final["engine_used"] == "sarvam"


def test_stt_stream_flush_existing_behavior(client):
    """Existing /v1/stt/stream WS flush-based path still works (regression guard)."""
    with client.websocket_connect("/v1/stt/stream") as ws:
        ws.send_text(json.dumps({"lang": "hi-en", "session_id": "legacy", "tenant_id": "t1"}))
        ws.send_bytes(b"\x00\x01" * 320)
        raw = ws.receive_text()
        result = json.loads(raw)
        assert result["text"] == "test transcript"
        assert result["is_final"] is True


# ── NEW /v1/stt/stream?lang=... query-param protocol tests ──────────────────


def test_stt_stream_new_protocol_pcm16_frames(client):
    """/v1/stt/stream with lang query param: PCM16 frames + empty sentinel → final event.

    This validates the NEW streaming contract:
      - Query params: lang=hi-en, session_id
      - Binary PCM16 8kHz frames (320 bytes = 20ms per frame)
      - Empty (0-byte) binary frame = end-of-utterance sentinel
      - Server sends {"type":"interim"} then {"type":"final"} JSON events
    """
    with client.websocket_connect(
        "/v1/stt/stream?lang=hi-en&session_id=new-stream-test"
    ) as ws:
        # Send 20ms PCM16 8kHz frames (320 bytes each)
        frame_20ms = b"\x00\x01" * 160  # 320 bytes = 20ms at 8kHz PCM16
        for _ in range(10):  # 200ms total
            ws.send_bytes(frame_20ms)
        # Empty sentinel signals end-of-utterance
        ws.send_bytes(b"")

        messages = []
        for _ in range(10):
            raw = ws.receive_text()
            msg = json.loads(raw)
            messages.append(msg)
            if msg.get("type") in ("final", "error"):
                break

    types = [m["type"] for m in messages]
    assert "final" in types, f"Expected 'final' in events; got: {messages}"
    final = next(m for m in messages if m["type"] == "final")
    assert final["text"] == "streaming test transcript"
    assert final["engine_used"] == "sarvam_streaming"


def test_stt_stream_new_protocol_interim_events(client):
    """/v1/stt/stream query-param mode: interim event arrives before final."""
    with client.websocket_connect(
        "/v1/stt/stream?lang=hi-en&session_id=interim-test"
    ) as ws:
        ws.send_bytes(b"\x00\x01" * 160)
        ws.send_bytes(b"")  # sentinel

        messages = []
        for _ in range(10):
            raw = ws.receive_text()
            msg = json.loads(raw)
            messages.append(msg)
            if msg.get("type") in ("final", "error"):
                break

    types = [m["type"] for m in messages]
    # FakeSarvamStreamingEngine emits interim then final
    assert "interim" in types, f"Expected interim; got: {types}"
    assert "final" in types, f"Expected final; got: {types}"
    # Interim must come before final
    assert types.index("interim") < types.index("final")


def test_stt_stream_new_protocol_empty_utterance(client):
    """/v1/stt/stream query-param mode: immediate empty sentinel returns empty final."""
    with client.websocket_connect(
        "/v1/stt/stream?lang=hi-en&session_id=empty-test"
    ) as ws:
        ws.send_bytes(b"")  # sentinel with no audio buffered

        raw = ws.receive_text()
        msg = json.loads(raw)

    assert msg["type"] == "final"
    assert msg["text"] == ""
    assert msg["engine_used"] == "none"


def test_stt_stream_new_protocol_disabled_flag(client_streaming_disabled):
    """/v1/stt/stream query-param mode with flag=disabled: error+close immediately."""
    import pytest
    from starlette.websockets import WebSocketDisconnect

    with pytest.raises((WebSocketDisconnect, Exception)):
        with client_streaming_disabled.websocket_connect(
            "/v1/stt/stream?lang=hi-en&session_id=disabled-test"
        ) as ws:
            raw = ws.receive_text()
            msg = json.loads(raw)
            assert msg["type"] == "error"
            assert msg["message"] == "streaming_disabled"
            # Server closes; next receive should raise
            ws.receive_text()


def test_stt_stream_new_protocol_legacy_unaffected(client_streaming_disabled):
    """Legacy /v1/stt/stream (no lang query param) works even when flag=disabled."""
    with client_streaming_disabled.websocket_connect("/v1/stt/stream") as ws:
        ws.send_text(json.dumps({"lang": "hi-en", "session_id": "legacy-disabled", "tenant_id": "t1"}))
        ws.send_bytes(b"\x00\x01" * 320)
        raw = ws.receive_text()
        result = json.loads(raw)
    assert result["text"] == "test transcript"
    assert result["is_final"] is True
