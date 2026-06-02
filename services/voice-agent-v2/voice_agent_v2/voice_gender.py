"""Speaker -> gender mapping for dynamic per-campaign voice.

Ported from services/voice-agent-worker/voice_agent/voice_gender.py.
Single source of truth for "which Sarvam bulbul:v3 speaker is male vs female".

Drives THREE things so the WHOLE conversation matches the chosen voice:
  1. the TTS speaker (ctx.voice_profile_id is the speaker id, used directly),
  2. the greeting text grammar (male / female verb forms),
  3. the LLM persona directive (you are a male/female telecaller).

NOTHING is hardcoded to one gender — everything derives from the campaign's
chosen voice id via gender_for_speaker().
"""

from __future__ import annotations

# bulbul:v3 male speakers (verified present in tts-router _VOICES_V3).
_MALE_SPEAKERS = frozenset({"rahul", "aditya", "rohan", "kabir"})
# bulbul:v3 female speakers (verified present in tts-router _VOICES_V3).
_FEMALE_SPEAKERS = frozenset({
    "priya", "neha", "pooja", "kavya", "simran",
    "ishita", "shreya", "ritu", "tanya", "suhani",
})

# When a speaker id is unknown, default to female only as last resort.
# The live default voice (rahul) is male and IS in the map.
_DEFAULT_GENDER = "female"


def gender_for_speaker(speaker: str | None) -> str:
    """Return "male" or "female" for a Sarvam speaker id (case-insensitive).

    Unknown / empty ids fall back to _DEFAULT_GENDER.
    """
    s = (speaker or "").strip().lower()
    if s in _MALE_SPEAKERS:
        return "male"
    if s in _FEMALE_SPEAKERS:
        return "female"
    return _DEFAULT_GENDER


def is_male(speaker: str | None) -> bool:
    return gender_for_speaker(speaker) == "male"
