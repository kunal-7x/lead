"""Speaker -> gender mapping for dynamic per-campaign voice.

Single source of truth for "which Sarvam bulbul:v3 speaker is male vs female".
Drives THREE things so the WHOLE conversation matches the chosen voice:
  1. the TTS speaker (ctx.voice_profile_id is the speaker id, used directly),
  2. the greeting text grammar (male "कर रहा हूँ" / female "कर रही हूँ"),
  3. the LLM persona directive (you are a male/female telecaller).

NOTHING is hardcoded to one gender — everything derives from the campaign's
chosen voice id via :func:`gender_for_speaker`.

Speaker ids verified against tts-router engines/sarvam.py `_VOICES_V3`.
"""

from __future__ import annotations

# bulbul:v3 male speakers (verified present in tts-router _VOICES_V3).
_MALE_SPEAKERS = frozenset({"rahul", "aditya", "rohan", "kabir"})
# bulbul:v3 female speakers (verified present in tts-router _VOICES_V3).
_FEMALE_SPEAKERS = frozenset({
    "priya", "neha", "pooja", "kavya", "simran",
    "ishita", "shreya", "ritu", "tanya", "suhani",
})

# When a speaker id is unknown (e.g. a legacy v2 voice or an empty string), we
# don't guess wrongly — default to female only as a last resort, but the live
# default voice (rahul, SARVAM_TTS_SPEAKER) is male and IS in the map, so this
# fallback is effectively never hit in production.
_DEFAULT_GENDER = "female"


def gender_for_speaker(speaker: str | None) -> str:
    """Return "male" or "female" for a Sarvam speaker id (case-insensitive).

    Unknown / empty ids fall back to _DEFAULT_GENDER. The mapping is the ONLY
    place gender is decided — greeting grammar and LLM persona both read it.
    """
    s = (speaker or "").strip().lower()
    if s in _MALE_SPEAKERS:
        return "male"
    if s in _FEMALE_SPEAKERS:
        return "female"
    return _DEFAULT_GENDER


def is_male(speaker: str | None) -> bool:
    return gender_for_speaker(speaker) == "male"
