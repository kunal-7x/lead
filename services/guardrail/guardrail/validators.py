from __future__ import annotations

import re
from dataclasses import dataclass, field

# Claim keywords that must be verified against KB before speaking
_CLAIM_PATTERNS = re.compile(
    r"\b(\d+%?\s*discount|possession\s+in\s+\d+|loan\s+available|"
    r"emi\s+of\s+\d+|price\s+is\s+\d+|ready\s+to\s+move|rera\s+approved)\b",
    re.IGNORECASE,
)

_INJECTION_PATTERNS = re.compile(
    r"(ignore\s+(previous|all)\s+instructions?|"
    r"you\s+are\s+now|jailbreak|forget\s+(you\s+are|your\s+instructions?)|"
    r"act\s+as\s+if|pretend\s+(you\s+are|to\s+be)|"
    r"disregard\s+(previous|all))",
    re.IGNORECASE,
)

_SAFE_HANDOVER = "Main aapko hamare specialist se connect karta hoon jo aapki poori madad karenge."
_MAX_WORDS = 40


@dataclass
class CheckResult:
    brain: dict
    action_taken: str = "none"
    reason: str = ""
    hallucination_detected: bool = False
    injection_detected: bool = False


def run_all(brain: dict, kb_chunks: list[str], user_turn: str = "") -> CheckResult:
    """Run all guardrail validators in order. Returns modified brain + audit info."""
    result = CheckResult(brain=dict(brain))
    actions = []

    # 1. Prompt injection (check user_turn for injection attempts)
    if _check_injection(user_turn, result):
        actions.append("injection_blocked")

    # 2. Unsafe gate
    if result.brain.get("risk_level") == "unsafe":
        _force_handover(result, "unsafe_risk_level")
        actions.append("unsafe_blocked")

    # 3. Claim control (check reply against KB)
    if _check_claims(result, kb_chunks):
        actions.append("claim_blocked")

    # 4. Hallucination check (facts in reply not in KB)
    if _check_hallucination(result, kb_chunks):
        actions.append("hallucination_blocked")

    # 5. Length cap
    if _cap_length(result):
        actions.append("length_capped")

    result.action_taken = ",".join(actions) if actions else "none"
    return result


def _check_injection(user_turn: str, result: CheckResult) -> bool:
    if _INJECTION_PATTERNS.search(user_turn or ""):
        result.injection_detected = True
        result.brain["reply"] = _SAFE_HANDOVER
        result.brain["should_handover_to_human"] = True
        result.brain["risk_level"] = "unsafe"
        result.brain["next_action"] = "handover"
        result.reason = "prompt injection detected in user input"
        return True
    return False


def _check_claims(result: CheckResult, kb_chunks: list[str]) -> bool:
    reply = result.brain.get("reply", "")
    matches = _CLAIM_PATTERNS.findall(reply)
    if not matches:
        return False

    kb_text = " ".join(kb_chunks).lower()
    blocked = False
    for claim in matches:
        # Check if any part of the claim phrase appears in KB
        if not any(word.lower() in kb_text for word in claim.split() if len(word) > 3):
            result.brain["reply"] = "Please verify with our team."
            result.brain["risk_level"] = "risky"
            result.reason = f"unverified claim: {claim}"
            blocked = True
            break
    return blocked


def _check_hallucination(result: CheckResult, kb_chunks: list[str]) -> bool:
    """Simple hallucination check: if reply mentions a number not in any KB chunk."""
    reply = result.brain.get("reply", "")
    kb_text = " ".join(kb_chunks)
    numbers_in_reply = set(re.findall(r"\b\d{4,}\b", reply))  # 4+ digit numbers (prices etc.)
    numbers_in_kb = set(re.findall(r"\b\d{4,}\b", kb_text))

    phantom = numbers_in_reply - numbers_in_kb
    if phantom and kb_chunks:  # only flag if KB was actually provided
        result.hallucination_detected = True
        result.brain["reply"] = "Please verify exact figures with our team."
        result.brain["risk_level"] = "risky"
        result.reason = f"hallucinated figures: {phantom}"
        return True
    return False


def _force_handover(result: CheckResult, reason: str) -> None:
    result.brain["reply"] = _SAFE_HANDOVER
    result.brain["should_handover_to_human"] = True
    result.brain["next_action"] = "handover"
    result.reason = reason


def _cap_length(result: CheckResult) -> bool:
    reply = result.brain.get("reply", "")
    words = reply.split()
    if len(words) > _MAX_WORDS:
        # Truncate to first 2 sentences
        sentences = re.split(r"(?<=[.!?])\s+", reply)
        result.brain["reply"] = " ".join(sentences[:2])
        return True
    return False
