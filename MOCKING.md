# Mocking & Demo Mode

Set `DEMO_MODE=true` in `.env` to route all third-party providers to local mocks.
Each provider gateway honors `DEMO_MODE` and `<PROVIDER>_MOCK=true` independently.

## Providers with mocks

| Provider     | Mock module                                  | Env flag           |
|--------------|----------------------------------------------|--------------------|
| Plivo        | `libs/go/mocks/plivo`                        | `PLIVO_MOCK`       |
| Meta WA      | `libs/go/mocks/whatsapp`                     | `META_WA_MOCK`     |
| Sarvam       | `libs/python/mocks/sarvam.py`                | `SARVAM_MOCK`      |
| Groq         | `libs/python/mocks/groq.py`                  | `GROQ_MOCK`        |
| ElevenLabs   | `libs/python/mocks/elevenlabs.py`            | `ELEVENLABS_MOCK`  |
| OpenRouter   | `libs/python/mocks/openrouter.py`            | `OPENROUTER_MOCK`  |

## Behaviors in demo mode

- Plivo calls return a fake call_uuid, emit synthetic webhooks via local NATS.
- WhatsApp messages stored in an in-memory inbox, retrievable via admin UI.
- ASR returns a fixed transcript from a fixture pool keyed by audio length.
- TTS returns a pre-rendered WAV from `fixtures/tts/`.
- LLM router returns deterministic responses from `fixtures/llm/`.
