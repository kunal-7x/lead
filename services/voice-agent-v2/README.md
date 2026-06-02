# voice-agent-v2

Clean LiveKit + Pipecat voice agent, rebuilt against the **real** installed API
(pipecat-ai==1.3.0). The old scaffold at `services/voice-agent-worker/voice_agent/pipecat_runtime/`
was written against a guessed API and never ran — this service uses only verified symbols.

## Real API symbols (verified by introspection, NOT guessed)

| What | REAL path | Old scaffold's wrong guess |
|------|-----------|---------------------------|
| Pipeline | `pipecat.pipeline.pipeline.Pipeline` | same (correct) |
| Worker | `pipecat.pipeline.task.PipelineWorker` | `PipelineWorker` (deprecated `PipelineTask` alias since 1.3.0) |
| PipelineParams | `pipecat.pipeline.worker.PipelineParams` | guessed wrong module |
| Runner | `pipecat.workers.runner.WorkerRunner` | `pipecat.workers.runner.WorkerRunner` (correct path) |
| LiveKit transport | `pipecat.transports.livekit.transport.LiveKitTransport` | `pipecat.transports.services.livekit` (DOESN'T EXIST) |
| LiveKit params | `LiveKitParams(audio_in_enabled, audio_out_enabled, audio_in_sample_rate, audio_out_sample_rate)` | wrong field names |
| VAD | `pipecat.audio.vad.silero.SileroVADAnalyzer` + `pipecat.processors.audio.vad_processor.VADProcessor` | same (correct) |
| Deepgram STT | `pipecat.services.deepgram.stt.DeepgramSTTService` | used Sarvam (not bundled in default install) |
| ElevenLabs TTS | `pipecat.services.elevenlabs.tts.ElevenLabsTTSService` | same, but `voice_id`/`model` kwargs deprecated → use `settings=ElevenLabsTTSService.Settings(voice=..., model=...)` |
| WorkerRunner init | needs running asyncio loop (`asyncio.get_running_loop()`) | not documented in scaffold |

### LiveKitTransport constructor
```python
LiveKitTransport(url: str, token: str, room_name: str, params: LiveKitParams | None = None)
```
Token is a LiveKit JWT generated via:
```python
from livekit.api import AccessToken, VideoGrants
token = AccessToken(api_key, api_secret).with_identity("voice-agent").with_grants(
    VideoGrants(room_join=True, room=room_name)
).to_jwt()
```

### PipelineWorker constructor (key params)
```python
PipelineWorker(pipeline, *, params=PipelineParams(...), enable_rtvi=False, enable_turn_tracking=False)
```

### WorkerRunner
```python
# Must be constructed inside a running asyncio loop
WorkerRunner(handle_sigint=False)
runner.run(worker)   # synchronous; starts asyncio loop
```

## Pipeline chain
```
transport.input() → VADProcessor(SileroVADAnalyzer) → DeepgramSTTService
    → EchoLLMProcessor [PLACEHOLDER] → ElevenLabsTTSService → transport.output()
```

VAD + turn detection are owned by the framework. `VADProcessor` handles barge-in
and fires `UserSpeakingFrame`. Pipecat's built-in turn detection decides when to
hand off to the LLM stage.

## What's stubbed
- **LLM stage**: `EchoLLMProcessor` echoes transcripts back as speech. The real
  `LlmRouterProcessor` (single streaming HTTP call to llm-router, no turn
  fragmentation, transcript forwarding) is the **next unit**.
- **Sarvam TTS**: not installed (requires `pipecat-ai[sarvam]`). Both providers
  use ElevenLabs for now; Sarvam wired in when `SARVAM_API_KEY` is set.

## Install

```powershell
# Windows (uv)
cd services/voice-agent-v2
uv venv .venv
uv pip install -r requirements.txt --python .\.venv\Scripts\python.exe
uv pip install -e . --python .\.venv\Scripts\python.exe

# Or standard pip
pip install -r requirements.txt
pip install -e .
```

## Run tests

```powershell
.\.venv\Scripts\python.exe -m pytest tests/ -v
# Expected: 38 passed
```

## Run locally (with real LiveKit)

```powershell
# 1. Start LiveKit dev server (Docker required)
docker run --rm -p 7880:7880 -p 7881:7881 -p 7882:7882/udp livekit/livekit-server --dev

# 2. Set env vars
copy .env.template .env
# Edit .env with real DEEPGRAM_API_KEY, ELEVENLABS_API_KEY, etc.

# 3. Run agent
.\.venv\Scripts\python.exe -m voice_agent_v2.agent
```

## Docker (optional local test)
```powershell
docker run --rm -p 7880:7880 livekit/livekit-server --dev
```
The agent will connect to `ws://localhost:7880` with a dev JWT token
(api_key=devkey, secret from env).
