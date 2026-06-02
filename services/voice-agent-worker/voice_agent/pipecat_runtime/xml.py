"""Vobiz Answer-URL XML builder.

Mirrors telephony-adapter's ``plivoxml.VobizStreamResponse`` shape EXACTLY so
Vobiz treats this endpoint identically to the existing media bridge:

    <Response>
      <Stream bidirectional="true" contentType="audio/x-mulaw;rate=8000"
              keepCallAlive="true" audioTrack="inbound">wss://…/pipecat/ws/{id}</Stream>
    </Response>

Pure string building (no Pipecat import) so it is trivially unit-testable.
"""

from __future__ import annotations

from xml.sax.saxutils import escape

# Same content type the existing bridge advertises (G.711 µ-law @ 8 kHz).
VOBIZ_CONTENT_TYPE = "audio/x-mulaw;rate=8000"


def build_stream_ws_url(public_ws_base: str, call_id: str) -> str:
    """Compose the wss URL Vobiz should open for this call.

    ``public_ws_base`` is the externally-reachable wss host (nginx fronts the
    127.0.0.1 app), e.g. ``wss://voice.example.com``. Trailing slash tolerated.
    """
    base = (public_ws_base or "").rstrip("/")
    return f"{base}/pipecat/ws/{call_id}"


def build_answer_xml(public_ws_base: str, call_id: str) -> str:
    """Return the Vobiz <Response><Stream>…</Stream></Response> XML string.

    Attribute set + order matches telephony-adapter/internal/plivoxml exactly:
    bidirectional, contentType, keepCallAlive, audioTrack. keepCallAlive keeps
    the PSTN leg bridged; audioTrack=inbound streams the caller's audio in while
    bidirectional still lets us play audio back.
    """
    ws_url = escape(build_stream_ws_url(public_ws_base, call_id))
    return (
        '<?xml version="1.0" encoding="UTF-8"?>'
        "<Response>"
        '<Stream bidirectional="true" '
        f'contentType="{VOBIZ_CONTENT_TYPE}" '
        'keepCallAlive="true" '
        f'audioTrack="inbound">{ws_url}</Stream>'
        "</Response>"
    )
