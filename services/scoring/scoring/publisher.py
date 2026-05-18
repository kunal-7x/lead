from __future__ import annotations

import os
from typing import Protocol


class Publisher(Protocol):
    async def publish(self, subject: str, payload: bytes) -> None: ...


class NatsPublisher:
    def __init__(self, url: str | None = None) -> None:
        self._url = url or os.environ.get("NATS_URL", "nats://localhost:4222")
        self._nc: object = None

    async def connect(self) -> None:
        import nats  # type: ignore[import]
        self._nc = await nats.connect(self._url)

    async def publish(self, subject: str, payload: bytes) -> None:
        if self._nc is None:
            raise RuntimeError("Not connected")
        await self._nc.publish(subject, payload)  # type: ignore[union-attr]


class FakePublisher:
    def __init__(self) -> None:
        self.published: list[tuple[str, bytes]] = []

    async def publish(self, subject: str, payload: bytes) -> None:
        self.published.append((subject, payload))

    def subjects(self) -> list[str]:
        return [s for s, _ in self.published]
