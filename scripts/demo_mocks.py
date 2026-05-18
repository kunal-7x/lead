from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class MockProvider:
    id: str
    latency_ms: int
    response: str


MOCK_PROVIDERS = {
    "mock-sarvam": MockProvider("mock-sarvam", 45, "haan, mujhe 2BHK mein interest hai"),
    "mock-groq": MockProvider("mock-groq", 55, "structured brain JSON fixture"),
    "mock-openrouter": MockProvider("mock-openrouter", 60, "fallback brain JSON fixture"),
    "mock-elevenlabs": MockProvider("mock-elevenlabs", 50, "8000hz-l16-demo-audio"),
    "mock-llm": MockProvider("mock-llm", 40, "deterministic handoff/site-visit decisions"),
}


def provider_response(provider_id: str) -> MockProvider:
    return MOCK_PROVIDERS[provider_id]
