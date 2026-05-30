"""Real NATS-backed event publisher + turn store for the voice-agent-worker.

These replace the demo JSONL stubs (demo_runtime.py) in production. They speak
exactly the protocols the brain already calls:

  * ``NATSEventPublisher`` implements ``actions.Publisher``
    (``async publish(subject: str, payload: bytes)``). The brain (actions.py and
    agent._finalize) publishes ``call.handover.requested``,
    ``call.site_visit.requested``, ``call.callback.requested``,
    ``call.whatsapp.requested``, ``lead.status.updated`` and the enriched
    ``call.completed`` through it.

  * ``NatsTurnStore`` implements ``recorder.TurnStore``
    (``record_turn(CallTurn)`` + ``complete_call(session_id, summary, outcome)``).
    Each turn is published as ``call.turn.recorded`` so analytics-sink ingests it
    into ``fact_call_turns``. ``complete_call`` is intentionally light — the
    enriched ``call.completed`` (with the full transcript) is emitted by the brain
    through the publisher, so we do not duplicate it here.

Wire format: the body is a flat JSON object, exactly what the Go consumers expect
(analytics-sink eventFromMessage uses the raw body as the canonical-event payload
and the NATS subject as the event type). A ``Nats-Msg-Id`` header is set on every
message so JetStream deduplicates redeliveries within its dedup window.

Durability: subjects land in the pre-existing JetStream streams (CAPSY_CALL for
``call.>``, CAPSY_LEAD for ``lead.>``) which the Go services already assert.

Resilience: NATS connection is established lazily and survives drops (infinite
reconnect). If NATS is unreachable, publishes are dropped with a log line — the
voice call must keep working even when the event bus is down.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import uuid
from dataclasses import asdict

import nats
from nats.aio.client import Client as NATSClient

from voice_agent.models import CallTurn

logger = logging.getLogger(__name__)

# JetStream uses this header for message deduplication.
_MSG_ID_HEADER = "Nats-Msg-Id"

_CONNECT_TIMEOUT_S = float(os.getenv("NATS_CONNECT_TIMEOUT_S", "5"))


class NATSEventPublisher:
    """Publishes brain events to NATS. Implements the actions.Publisher protocol.

    A single connection is shared process-wide and created lazily on first
    publish. Connection failures degrade gracefully: the publish is dropped and
    logged, never raised, so a NATS outage can never break a live call.
    """

    # Class-level shared connection so every call reuses one NATS connection
    # instead of opening one per call.
    _shared_nc: NATSClient | None = None
    _connect_lock: asyncio.Lock = asyncio.Lock()
    _connect_failed: bool = False

    def __init__(self, nats_url: str | None = None) -> None:
        self._url = nats_url or os.getenv("NATS_URL", "nats://localhost:4222")

    async def _ensure_connected(self) -> NATSClient | None:
        cls = NATSEventPublisher
        nc = cls._shared_nc
        if nc is not None and nc.is_connected:
            return nc
        async with cls._connect_lock:
            # Re-check inside the lock (another coroutine may have connected).
            nc = cls._shared_nc
            if nc is not None and nc.is_connected:
                return nc
            try:
                nc = await nats.connect(
                    self._url,
                    name="voice-agent-worker",
                    connect_timeout=_CONNECT_TIMEOUT_S,
                    max_reconnect_attempts=-1,  # reconnect forever
                    reconnect_time_wait=2,
                    allow_reconnect=True,
                )
                cls._shared_nc = nc
                cls._connect_failed = False
                logger.info("NATSEventPublisher connected to %s", self._url)
                return nc
            except Exception as exc:  # noqa: BLE001
                # Degrade gracefully — do not crash the call.
                if not cls._connect_failed:
                    logger.warning(
                        "NATSEventPublisher could not connect to %s (%r); "
                        "events will be dropped until NATS is reachable",
                        self._url, exc,
                    )
                cls._connect_failed = True
                return None

    async def publish(self, subject: str, payload: bytes) -> None:
        nc = await self._ensure_connected()
        if nc is None:
            return  # NATS down — drop event, call continues.
        try:
            headers = {_MSG_ID_HEADER: f"{subject}:{uuid.uuid4().hex}"}
            await nc.publish(subject, payload, headers=headers)
        except Exception as exc:  # noqa: BLE001
            logger.warning("NATS publish failed subject=%s (%r)", subject, exc)

    async def aclose(self) -> None:
        cls = NATSEventPublisher
        nc = cls._shared_nc
        if nc is not None:
            try:
                await nc.drain()
            except Exception:  # noqa: BLE001
                pass
            cls._shared_nc = None


class NatsTurnStore:
    """Persists per-turn transcript by publishing call.turn.recorded to NATS.

    Implements the recorder.TurnStore protocol. Each turn becomes a flat JSON
    event consumed by analytics-sink (fact_call_turns). complete_call is light:
    the enriched call.completed (carrying the full transcript) is published by the
    brain through NATSEventPublisher, so we avoid emitting a duplicate here.
    """

    def __init__(self, publisher: NATSEventPublisher | None = None) -> None:
        self._publisher = publisher or NATSEventPublisher()

    async def record_turn(self, turn: CallTurn) -> None:
        payload = asdict(turn)
        # confidence drives the analytics turn_score metric; keep brain_json for
        # the post-call pipeline but it stays inside the flat object.
        await self._publisher.publish(
            "call.turn.recorded",
            json.dumps(payload, ensure_ascii=True).encode(),
        )

    async def complete_call(self, session_id: str, summary: str, outcome: str) -> None:
        # call.completed (enriched with transcript) is emitted by the brain's
        # _finalize via the publisher. Nothing extra to persist here.
        return None
