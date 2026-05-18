from __future__ import annotations

from scoring.models import LeadHistory, ScoreResult


class FakeStore:
    def __init__(self) -> None:
        self._rows: dict[str, list[ScoreResult]] = {}

    async def save(self, result: ScoreResult) -> None:
        self._rows.setdefault(result.lead_id, []).append(result)

    async def get_history(self, lead_id: str) -> LeadHistory:
        rows = self._rows.get(lead_id, [])
        return LeadHistory(
            lead_id=lead_id,
            latest=rows[-1] if rows else None,
            history=rows,
        )
