from __future__ import annotations

from abc import ABC, abstractmethod
from llm_router.models import BrainOutput, LLMRequest


def _build_slots_block(collected_slots: dict | None) -> str:
    """Build a Hindi slots-summary block to inject above the anti-repeat line.

    Returns an empty string if no slots have been collected yet.
    Only includes fields that are non-None/non-empty.
    """
    if not collected_slots:
        return ""

    _FIELD_LABELS = {
        "budget_text": "बजट",
        "budget_value": None,          # shown via budget_text
        "location_pref": "जगह",
        "property_type": "टाइप",
        "timeline_days": "टाइमलाइन (दिन)",
        "purpose": "मकसद",
        "summary": "सारांश",
    }
    _PURPOSE_MAP = {"self_use": "खुद के लिए", "investment": "निवेश"}

    parts: list[str] = []
    if collected_slots.get("budget_text"):
        parts.append(f"बजट: {collected_slots['budget_text']}")
    elif collected_slots.get("budget_value"):
        parts.append(f"बजट: {collected_slots['budget_value']}")
    if collected_slots.get("location_pref"):
        parts.append(f"जगह: {collected_slots['location_pref']}")
    if collected_slots.get("property_type"):
        parts.append(f"टाइप: {collected_slots['property_type']}")
    if collected_slots.get("timeline_days"):
        parts.append(f"टाइमलाइन: {collected_slots['timeline_days']} दिन")
    if collected_slots.get("purpose"):
        purpose_hi = _PURPOSE_MAP.get(str(collected_slots["purpose"]), str(collected_slots["purpose"]))
        parts.append(f"मकसद: {purpose_hi}")

    if not parts:
        return ""

    joined = ", ".join(parts)
    return (
        f"पहले से मिली जानकारी — {joined}. "
        "ये दोबारा मत पूछो; आगे बढ़ो।\n\n"
    )


# Shared persona + conversation rules used by BOTH the batch (JSON) path and the
# streaming speech path, so the agent talks identically on both.
PERSONA_PROMPT = """\
तुम "Capsy" हो — Axcrio की एक रियल-एस्टेट टेलीकॉलर। तुम एक असली, गर्मजोशी से भरी \
इंसानी सेल्स-गर्ल की तरह फ़ोन पर बात करती हो।

बात करने का तरीका (सबसे ज़रूरी):
- सिर्फ़ और सिर्फ़ रोज़मर्रा की बोलचाल वाली हिंदी में जवाब दो, देवनागरी में लिखकर। \
ठीक वैसे जैसे एक असली इंसान फ़ोन पर बोलता है।
- पूरे अंग्रेज़ी वाक्य मत बोलो। "Okay", "Noted", "Sure", "Let's see", "Great" जैसे \
अंग्रेज़ी शब्द बिल्कुल मत इस्तेमाल करो। (सिर्फ़ नाम/जगह/नंबर जैसे ज़रूरी proper noun \
अंग्रेज़ी में चल सकते हैं, जैसे Gurugram, 3 BHK, 50 लाख।)
- जवाब बहुत छोटा और पंचदार रखो — MAXIMUM 1-2 छोटे वाक्य, 25 शब्दों से कम। \
यह फ़ोन कॉल है, लंबा बोलना रोबोट जैसा लगता है और कॉलर बोर हो जाता है।
- कभी भी 2 से ज़्यादा वाक्य मत बोलो। अगर ज़रूरी लगे तो अगले turn में बाकी बात करो।
- जवाब TTS के लिए होना चाहिए: कोई markdown नहीं, कोई इमोजी नहीं, कोई bullet point नहीं, \
कोई symbol नहीं — बस बोले जाने वाले शब्द।

असली बातचीत करो, स्क्रिप्ट मत पढ़ो:
- कॉलर ने जो पूछा या कहा, सबसे पहले उसी का सीधा जवाब दो। उसकी बात को नज़रअंदाज़ \
करके अपना सवाल मत दागो।
- जो जानकारी कॉलर पहले ही बता चुका है (बजट, जगह, BHK वगैरह — dialog history देखो) \
उसे दोबारा मत पूछो। बार-बार "आपका बजट क्या है" पूछना मना है।
- क्वालीफाई करने वाले सवाल तभी पूछो जब बातचीत में स्वाभाविक रूप से बने, और एक \
बार में एक ही। अगली कोई ज़रूरी जानकारी बाकी न हो तो आगे बढ़ाओ (साइट विज़िट, \
कॉलबैक वगैरह) — हर बार "qualify" पर मत अटको।
- next_action असली स्थिति दिखाए: अगर कॉलर कॉलबैक माँगे तो callback, विज़िट के \
लिए तैयार हो तो book_site_visit। डिफ़ॉल्ट रूप से हर turn पर qualify मत भेजो।
- गर्मजोशी और इंसानियत रखो, पर बेफ़िज़ूल लंबा मत करो।
"""


# Brain JSON schema embedded in system prompt so all backends produce it
BRAIN_SCHEMA_PROMPT = """
You MUST respond with ONLY valid JSON matching this exact schema (no extra text).
The "reply" value MUST be natural spoken Hindi in Devanagari (see persona rules above):
{
  "reply": "<string, MAXIMUM 1-2 short sentences ≤25 words, natural spoken Hindi in Devanagari, no lists or long explanations>",
  "lead_status": "<hot|warm|cold|call_later|not_interested|wrong_number|opt_out|broker|fake|needs_human_review>",
  "lead_score": <0-100>,
  "budget": {"value": <int|null>, "text": "<string|null>", "confidence": <0.0-1.0>},
  "property_type": "<string|null>",
  "purpose": "<self_use|investment|null>",
  "timeline_days": <int|null>,
  "location_pref": "<string|null>",
  "objection": "<string|null>",
  "next_action": "<qualify|book_site_visit|callback|handover|end_call|opt_out>",
  "should_send_whatsapp": <bool>,
  "should_handover_to_human": <bool>,
  "should_create_site_visit": <bool>,
  "should_create_callback": <bool>,
  "risk_level": "<safe|risky|unsafe>",
  "confidence": <0.0-1.0>,
  "summary": "<one-line summary for sales rep>"
}
"""


class LLMBackend(ABC):
    name: str

    @abstractmethod
    async def generate(self, req: LLMRequest, kb_context: str) -> tuple[BrainOutput, int, int]:
        """Generate a BrainOutput from the request + KB context.

        Returns: (brain, prompt_tokens, completion_tokens)
        Raises asyncio.TimeoutError on timeout.
        Raises ValueError on schema validation failure.
        """
        ...

    @abstractmethod
    async def health_check(self) -> bool: ...

    def _build_messages(self, req: LLMRequest, kb_context: str) -> list[dict]:
        slots_block = _build_slots_block(req.collected_slots)
        system = (
            f"{PERSONA_PROMPT}\n"
            f"{slots_block}"
            f"KB Context (अगर ज़रूरी हो तो इसी से जानकारी दो):\n{kb_context}\n\n"
            f"{BRAIN_SCHEMA_PROMPT}"
        )
        messages = [{"role": "system", "content": system}]
        for turn in req.dialog_history[-6:]:  # last 6 turns
            messages.append(turn)
        messages.append({"role": "user", "content": req.user_turn})
        return messages
