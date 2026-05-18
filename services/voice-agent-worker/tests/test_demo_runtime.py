from __future__ import annotations

import json

from voice_agent.demo_runtime import DemoEventPublisher, DemoTurnStore
from voice_agent.models import CallTurn


async def test_demo_event_publisher_writes_jsonl(tmp_path):
    publisher = DemoEventPublisher(tmp_path)

    await publisher.publish("call.whatsapp.requested", b'{"session_id":"sess-1"}')

    lines = (tmp_path / "events.jsonl").read_text(encoding="utf-8").splitlines()
    assert len(lines) == 1
    event = json.loads(lines[0])
    assert event["subject"] == "call.whatsapp.requested"
    assert event["payload"]["session_id"] == "sess-1"


async def test_demo_turn_store_writes_turns_and_completion(tmp_path):
    store = DemoTurnStore(tmp_path)

    await store.record_turn(
        CallTurn(
            session_id="sess-1",
            turn_index=0,
            speaker="caller",
            transcript="budget 60 lakh",
            reply="Site visit book kar dete hain.",
            lead_status="hot",
            next_action="book_site_visit",
            brain_json={"confidence": 0.9},
            tts_engine="sarvam_bulbul",
            cache_hit=False,
        )
    )
    await store.complete_call("sess-1", "Hot lead", "book_site_visit")

    turn = json.loads((tmp_path / "turns.jsonl").read_text(encoding="utf-8").splitlines()[0])
    completed = json.loads(
        (tmp_path / "completed_calls.jsonl").read_text(encoding="utf-8").splitlines()[0]
    )

    assert turn["session_id"] == "sess-1"
    assert turn["brain_json"]["confidence"] == 0.9
    assert completed == {
        "session_id": "sess-1",
        "summary": "Hot lead",
        "outcome": "book_site_visit",
    }

