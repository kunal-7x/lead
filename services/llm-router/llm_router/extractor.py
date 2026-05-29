"""Campaign context extractor — turns raw brief text into a CampaignContext dict."""
from __future__ import annotations

import json
import os
from typing import Any

import httpx

_SYSTEM_PROMPT = (
    "You extract structured outbound-call campaign fields from a raw brief. "
    "Return ONLY a JSON object with EXACTLY these keys: "
    "product_description, offer, talking_points[], "
    "objection_handling[{objection,response}], qualifying_questions[], "
    "persona, do_not_say[], goal, language, business_hours. "
    "Use empty string/array when the brief doesn't mention a field. "
    "Do not invent compliance claims."
)

_EMPTY_CONTEXT: dict[str, Any] = {
    "product_description": "",
    "offer": "",
    "talking_points": [],
    "objection_handling": [],
    "qualifying_questions": [],
    "persona": "",
    "do_not_say": [],
    "goal": "",
    "language": "",
    "business_hours": "",
}


def _coerce(raw: dict[str, Any]) -> dict[str, Any]:
    """Coerce a raw LLM dict into the CampaignContext shape with safe defaults."""
    result = dict(_EMPTY_CONTEXT)
    str_keys = {"product_description", "offer", "persona", "goal", "language", "business_hours"}
    list_keys = {"talking_points", "qualifying_questions", "do_not_say"}

    for k in str_keys:
        v = raw.get(k, "")
        result[k] = v if isinstance(v, str) else str(v) if v else ""

    for k in list_keys:
        v = raw.get(k, [])
        if isinstance(v, list):
            result[k] = [str(i) for i in v]
        elif isinstance(v, str) and v:
            result[k] = [v]
        else:
            result[k] = []

    # objection_handling: list of {objection, response}
    oh = raw.get("objection_handling", [])
    if isinstance(oh, list):
        coerced_oh = []
        for item in oh:
            if isinstance(item, dict):
                coerced_oh.append({
                    "objection": str(item.get("objection", "")),
                    "response": str(item.get("response", "")),
                })
        result["objection_handling"] = coerced_oh
    else:
        result["objection_handling"] = []

    return result


class CampaignExtractor:
    """Calls an OpenRouter-compatible endpoint in JSON mode to extract campaign fields."""

    _DEFAULT_BASE_URL = "https://openrouter.ai/api/v1/chat/completions"
    _DEFAULT_MODEL = "google/gemini-2.0-flash-001"

    def __init__(self) -> None:
        self._api_key = (
            os.getenv("LLM_API_KEY")
            or os.getenv("OPENROUTER_API_KEY")
            or os.getenv("GROQ_API_KEY", "")
        )
        self._model = os.getenv("LLM_MODEL", self._DEFAULT_MODEL)
        self._base_url = os.getenv("LLM_BASE_URL", self._DEFAULT_BASE_URL)
        self._client = httpx.AsyncClient(
            timeout=30.0,
            limits=httpx.Limits(max_connections=100, max_keepalive_connections=20),
        )

    async def extract(self, text: str, schema_hint: str | None = None) -> dict[str, Any]:
        """Return a CampaignContext dict extracted from `text`."""
        system = _SYSTEM_PROMPT
        if schema_hint:
            system = f"{system}\nAdditional hint: {schema_hint}"

        payload: dict[str, Any] = {
            "model": self._model,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": text},
            ],
            "response_format": {"type": "json_object"},
            "temperature": 0.2,
        }
        headers = {"Authorization": f"Bearer {self._api_key}"}
        resp = await self._client.post(self._base_url, json=payload, headers=headers)
        resp.raise_for_status()
        body = resp.json()
        content = body["choices"][0]["message"]["content"]
        raw = json.loads(content)
        return _coerce(raw)

    async def aclose(self) -> None:
        await self._client.aclose()
