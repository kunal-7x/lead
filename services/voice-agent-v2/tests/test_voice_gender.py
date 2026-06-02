"""Tests for voice_gender module."""

from __future__ import annotations

from voice_agent_v2.voice_gender import gender_for_speaker, is_male


def test_known_male_speaker():
    assert gender_for_speaker("rahul") == "male"
    assert gender_for_speaker("aditya") == "male"


def test_known_female_speaker():
    assert gender_for_speaker("priya") == "female"
    assert gender_for_speaker("neha") == "female"


def test_case_insensitive():
    assert gender_for_speaker("RAHUL") == "male"
    assert gender_for_speaker("Priya") == "female"


def test_unknown_speaker_defaults_to_female():
    assert gender_for_speaker("unknown_voice_xyz") == "female"


def test_none_defaults_to_female():
    assert gender_for_speaker(None) == "female"


def test_empty_string_defaults_to_female():
    assert gender_for_speaker("") == "female"


def test_is_male():
    assert is_male("rahul") is True
    assert is_male("priya") is False
    assert is_male(None) is False
