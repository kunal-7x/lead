from __future__ import annotations

import asyncio
import json
import os
from dataclasses import asdict
from pathlib import Path
from typing import Any

from voice_agent.models import CallTurn


_ROOT = Path(__file__).resolve().parents[3]
_DEFAULT_DIR = _ROOT / ".demo" / "voice-agent"


def demo_dir() -> Path:
    return Path(os.getenv("VOICE_DEMO_DIR", str(_DEFAULT_DIR)))


class DemoEventPublisher:
    """JSONL-backed event publisher for the demo product flow."""

    def __init__(self, out_dir: Path | None = None) -> None:
        self._out_dir = out_dir or demo_dir()
        self._lock = asyncio.Lock()

    async def publish(self, subject: str, payload: bytes) -> None:
        entry = {"subject": subject, "payload": _decode_payload(payload)}
        await _append_jsonl(self._out_dir / "events.jsonl", entry, self._lock)


class DemoTurnStore:
    """JSONL-backed call turn store for demo runtime evidence."""

    def __init__(self, out_dir: Path | None = None) -> None:
        self._out_dir = out_dir or demo_dir()
        self._lock = asyncio.Lock()

    async def record_turn(self, turn: CallTurn) -> None:
        await _append_jsonl(self._out_dir / "turns.jsonl", asdict(turn), self._lock)

    async def complete_call(self, session_id: str, summary: str, outcome: str) -> None:
        await _append_jsonl(
            self._out_dir / "completed_calls.jsonl",
            {"session_id": session_id, "summary": summary, "outcome": outcome},
            self._lock,
        )


def _decode_payload(payload: bytes) -> Any:
    try:
        return json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return payload.decode("utf-8", errors="replace")


async def _append_jsonl(path: Path, entry: dict[str, Any], lock: asyncio.Lock) -> None:
    async with lock:
        path.parent.mkdir(parents=True, exist_ok=True)
        line = json.dumps(entry, ensure_ascii=True, sort_keys=True) + "\n"
        await asyncio.to_thread(_write_line, path, line)


def _write_line(path: Path, line: str) -> None:
    with path.open("a", encoding="utf-8") as fh:
        fh.write(line)

