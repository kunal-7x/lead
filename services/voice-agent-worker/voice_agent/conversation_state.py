"""Adaptive Conversational Intelligence — T2.1.

Conversation State Object (CSO) + Response Strategy Planner (RSP).

The CSO is a small per-session state dict inferred EACH turn from the recent
dialog via a FAST parallel Groq classification call.  The call is fired BEFORE
the main generation warmup and collected after (adds ~0 latency to first-audio).
On any error the module falls back to neutral defaults silently — it never
blocks a turn.

The RSP maps the CSO to a short per-turn DIRECTIVE string that is APPENDED to
the generation system prompt for THIS turn only, nudging tone/length/energy
without hard-coding a mode.

CSO → prosody wiring: optionally derives per-turn pace/temperature for
AgentLoop.set_turn_prosody().

Usage:
    engine = ConversationStateEngine(groq_api_key="...", groq_api_key_2="...")
    # Fire classification in parallel (before main LLM warmup):
    cso_task = asyncio.create_task(engine.update(dialog_history, user_turn))
    # ... main LLM path starts here ...
    # Collect result (non-blocking, falls back on error):
    await asyncio.wait_for(cso_task, timeout=1.5)   # or just fire-and-forget
    directive = engine.directive()
    prosody  = engine.prosody_params()
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import time

import httpx

logger = logging.getLogger(__name__)

# ── Classification model ───────────────────────────────────────────────────────
# Use the cheapest/fastest Groq model for state classification.  llama-3.1-8b-instant
# is free-tier, ~700 tok/s — ideal for a tiny JSON classification call that must
# resolve in <300 ms to stay latency-neutral.
_CSO_GROQ_URL = "https://api.groq.com/openai/v1/chat/completions"
_CSO_MODEL = os.getenv("CSO_MODEL", "llama-3.1-8b-instant")
_CSO_TIMEOUT_S = float(os.getenv("CSO_TIMEOUT_S", "1.5"))
# Disable CSO entirely: set CSO_ENABLED=false (useful for A/B testing).
_CSO_ENABLED = os.getenv("CSO_ENABLED", "true").lower() == "true"

# ── Neutral defaults (used on error / first turn / disabled) ──────────────────
_NEUTRAL_CSO: dict = {
    "user_mood": "casual",
    "user_patience": 0.6,
    "conversation_energy": 0.5,
    "sales_stage": "awareness",
    "response_density_target": "medium",
    "warmth": 0.6,
}

# ── Classification system prompt (JSON output) ────────────────────────────────
_CLASSIFY_SYSTEM = """You are a conversation state classifier for a Hindi real-estate telecaller AI.
Given the recent dialog and the last user message, output ONLY valid JSON (no prose) with these fields:

{
  "user_mood": "<one of: curious|impatient|skeptical|interested|confused|casual>",
  "user_patience": <float 0.0-1.0; 0=very impatient, 1=very patient>,
  "conversation_energy": <float 0.0-1.0; 0=low/bored, 1=high/engaged>,
  "sales_stage": "<one of: awareness|interest|consideration|intent|close>",
  "response_density_target": "<one of: short|medium|full>",
  "warmth": <float 0.0-1.0; 0=cold/distant, 1=warm/friendly>
}

Rules:
- impatient OR short utterance → response_density_target=short
- curious OR asking many questions → response_density_target=full
- skeptical → response_density_target=medium with warmth 0.7+
- confused → response_density_target=full, conversation_energy low
- casual greeting/chitchat → response_density_target=short
- Never output anything except the JSON object.
"""

# ── RSP directive templates ───────────────────────────────────────────────────
# Short Hindi+English directive strings injected as system-prompt suffix.
# The generation LLM reads these and naturally adjusts tone/length.
_DIRECTIVE_MAP: dict[str, dict[str, str]] = {
    # response_density_target → mood overrides
    "short": {
        "impatient":  "इस turn: बिल्कुल संक्षिप्त जवाब दें, 1-2 वाक्य max। ग्राहक जल्दी में है।",
        "casual":     "इस turn: छोटा, दोस्ताना जवाब दें।",
        "skeptical":  "इस turn: संक्षिप्त पर भरोसेमंद जवाब दें।",
        "_default":   "इस turn: संक्षिप्त जवाब दें।",
    },
    "medium": {
        "curious":    "इस turn: स्पष्ट और उपयोगी जवाब दें, ग्राहक जानना चाहता है।",
        "skeptical":  "इस turn: विश्वसनीय तथ्यों के साथ गर्मजोशी से जवाब दें।",
        "interested": "इस turn: उत्साह के साथ जानकारी दें, lead को engage रखें।",
        "_default":   "इस turn: संतुलित जवाब दें।",
    },
    "full": {
        "curious":    "इस turn: विस्तार से समझाएँ, ग्राहक उत्सुक है। सभी relevant details दें।",
        "confused":   "इस turn: धीरे-धीरे, सरल शब्दों में समझाएँ। confusion दूर करें।",
        "interested": "इस turn: पूरी जानकारी दें, ग्राहक interested है — इसे convert करने का मौका है।",
        "_default":   "इस turn: विस्तार से जवाब दें।",
    },
}

# ── Prosody param lookup (pace, temperature) ──────────────────────────────────
# Maps mood → (pace_multiplier, temperature_delta)
# Keep deltas small so they blend naturally with defaults (1.0, 0.6).
_PROSODY_MAP: dict[str, tuple[float, float]] = {
    "impatient":  (1.10, 0.55),   # slightly faster, slightly crisper
    "curious":    (0.95, 0.65),   # slightly slower, more expressive
    "interested": (1.00, 0.65),   # normal pace, more expressive
    "skeptical":  (0.95, 0.60),   # measured, calm
    "confused":   (0.90, 0.55),   # slow and clear
    "casual":     (1.00, 0.60),   # defaults
}


class ConversationStateEngine:
    """Per-session CSO engine.  Thread-safe for asyncio single-thread use.

    Attributes
    ----------
    _cso : dict
        The current (last-inferred or default) Conversation State Object.
    _last_classify_ms : float
        Wall-clock ms of the last classification call (for latency diag).
    """

    def __init__(
        self,
        groq_api_key: str = "",
        groq_api_key_2: str = "",
        timeout: float = _CSO_TIMEOUT_S,
    ) -> None:
        k1 = groq_api_key or os.getenv("GROQ_API_KEY", "")
        k2 = groq_api_key_2 or os.getenv("GROQ_API_KEY_2", "")
        self._keys = [k for k in (k1, k2) if k] or [""]
        self._timeout = timeout
        self._client: httpx.AsyncClient | None = None  # lazy-init to avoid issues in tests
        self._cso: dict = dict(_NEUTRAL_CSO)
        self._last_classify_ms: float = 0.0
        self._enabled = _CSO_ENABLED

    def _get_client(self) -> httpx.AsyncClient:
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=self._timeout)
        return self._client

    async def aclose(self) -> None:
        """Close the underlying HTTP client (call on session teardown)."""
        if self._client is not None:
            await self._client.aclose()
            self._client = None

    # ── Core API ───────────────────────────────────────────────────────────────

    async def update(
        self,
        dialog_history: list[dict],
        user_turn: str,
    ) -> dict:
        """Classify the current turn and update internal CSO.

        MUST be called as an asyncio.Task started BEFORE the main LLM warmup
        so it runs in parallel.  Errors fall back to the previous CSO (or
        neutral defaults on first turn) — never raises.

        Returns the new CSO dict.
        """
        if not self._enabled:
            return dict(self._cso)

        t0 = time.time()
        try:
            new_cso = await asyncio.wait_for(
                self._classify(dialog_history, user_turn),
                timeout=self._timeout,
            )
            self._cso = new_cso
        except (asyncio.TimeoutError, Exception) as exc:
            logger.debug("CSO classification failed (%r) — using previous state", exc)
            # Keep previous CSO (or neutral defaults if first turn)
        self._last_classify_ms = (time.time() - t0) * 1000
        logger.debug(
            "CSO update done in %.0f ms: mood=%s density=%s",
            self._last_classify_ms,
            self._cso.get("user_mood"),
            self._cso.get("response_density_target"),
        )
        return dict(self._cso)

    def directive(self) -> str:
        """Return the per-turn directive string for the generation system prompt.

        This is a SHORT Hindi instruction appended as a SUFFIX to the existing
        system prompt.  It makes length/tone EMERGENT (the LLM decides HOW to
        comply) rather than a hardcoded mode.

        Returns empty string when CSO is neutral/defaults (no unnecessary noise).
        """
        cso = self._cso
        density = cso.get("response_density_target", "medium")
        mood = cso.get("user_mood", "casual")

        density_map = _DIRECTIVE_MAP.get(density, _DIRECTIVE_MAP["medium"])
        return density_map.get(mood, density_map["_default"])

    def prosody_params(self) -> tuple[float | None, float | None]:
        """Return (pace, temperature) for set_turn_prosody(), or (None, None) if neutral.

        None means "use the existing default" — only overrides when the CSO
        suggests a meaningful departure.
        """
        cso = self._cso
        mood = cso.get("user_mood", "casual")
        patience = cso.get("user_patience", 0.6)

        if mood not in _PROSODY_MAP:
            return None, None

        pace, temp = _PROSODY_MAP[mood]

        # If patience is very low (<0.3), push pace a touch faster regardless of mood
        if patience < 0.3:
            pace = max(pace, 1.05)

        # Round to avoid floating-point noise in logs
        return round(pace, 2), round(temp, 2)

    def current_cso(self) -> dict:
        """Return a snapshot of the current CSO (for diag/logging)."""
        return dict(self._cso)

    # ── Internal: Groq classification call ────────────────────────────────────

    async def _classify(
        self, dialog_history: list[dict], user_turn: str
    ) -> dict:
        """Fire a lightweight Groq JSON classification call and return parsed CSO."""
        # Build a compact dialog context (last 6 turns max to keep tokens tiny)
        recent = dialog_history[-6:] if len(dialog_history) > 6 else dialog_history
        context_lines = []
        for msg in recent:
            role = "ग्राहक" if msg.get("role") == "user" else "AI"
            context_lines.append(f"{role}: {msg.get('content', '')[:120]}")
        context_lines.append(f"ग्राहक (अभी): {user_turn[:200]}")
        dialog_text = "\n".join(context_lines)

        messages = [
            {"role": "system", "content": _CLASSIFY_SYSTEM},
            {"role": "user", "content": f"Dialog:\n{dialog_text}"},
        ]
        payload = {
            "model": _CSO_MODEL,
            "messages": messages,
            "response_format": {"type": "json_object"},
            "temperature": 0.1,
            "max_tokens": 120,
        }

        last_exc: Exception | None = None
        client = self._get_client()
        for key in self._keys:
            headers = {"Authorization": f"Bearer {key}"}
            try:
                resp = await client.post(
                    _CSO_GROQ_URL, json=payload, headers=headers,
                    timeout=self._timeout,
                )
                if resp.status_code == 429:
                    last_exc = RuntimeError(f"Groq 429 key={key[:8]}...")
                    continue
                resp.raise_for_status()
                body = resp.json()
                content = body["choices"][0]["message"]["content"]
                raw = json.loads(content)
                return _validate_cso(raw)
            except Exception as exc:  # noqa: BLE001
                last_exc = exc
                continue

        raise last_exc or RuntimeError("All CSO Groq keys failed")


# ── Validation helpers ────────────────────────────────────────────────────────

_VALID_MOODS = frozenset({"curious", "impatient", "skeptical", "interested", "confused", "casual"})
_VALID_STAGES = frozenset({"awareness", "interest", "consideration", "intent", "close"})
_VALID_DENSITIES = frozenset({"short", "medium", "full"})


def _validate_cso(raw: dict) -> dict:
    """Clamp and validate a raw CSO dict from the LLM; fall back field-by-field."""
    out = dict(_NEUTRAL_CSO)  # start from neutral defaults

    mood = raw.get("user_mood", "")
    if mood in _VALID_MOODS:
        out["user_mood"] = mood

    patience = raw.get("user_patience", None)
    if isinstance(patience, (int, float)):
        out["user_patience"] = float(max(0.0, min(1.0, patience)))

    energy = raw.get("conversation_energy", None)
    if isinstance(energy, (int, float)):
        out["conversation_energy"] = float(max(0.0, min(1.0, energy)))

    stage = raw.get("sales_stage", "")
    if stage in _VALID_STAGES:
        out["sales_stage"] = stage

    density = raw.get("response_density_target", "")
    if density in _VALID_DENSITIES:
        out["response_density_target"] = density

    warmth = raw.get("warmth", None)
    if isinstance(warmth, (int, float)):
        out["warmth"] = float(max(0.0, min(1.0, warmth)))

    return out
