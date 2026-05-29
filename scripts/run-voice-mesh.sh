#!/usr/bin/env bash
# Start the AI voice mesh locally with low-latency streaming enabled.
# Usage: bash scripts/run-voice-mesh.sh   (run from codebase/)
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
set -a; source .env.ai; set +a
export REDIS_URL="redis://localhost:6379/0"
# low-latency streaming (the big concurrency win)
export STT_STREAMING_ENGINE="sarvam"
export TTS_STREAMING_WS="true"
# router URLs the worker calls
export STT_ROUTER_URL="http://localhost:8110"
export LLM_ROUTER_URL="http://localhost:8111"
export TTS_ROUTER_URL="http://localhost:8113"
export GUARDRAIL_URL="http://localhost:8114"

start() { # name dir module port extra...
  local name=$1 dir=$2 mod=$3 port=$4; shift 4
  ( cd "$ROOT/services/$dir" && uv run uvicorn "$mod" --host 0.0.0.0 --port "$port" "$@" \
      > "/tmp/$name.log" 2>&1 & echo "  $name pid $! :$port" )
}

echo "starting voice mesh..."
start stt-router    stt-router         stt_router.app:app   8110 --workers 2
start llm-router    llm-router         llm_router.app:app   8111 --workers 2
start tts-router    tts-router         tts_router.app:app   8113 --workers 2
start guardrail     guardrail          guardrail.app:app    8114 --workers 1
start voice-worker  voice-agent-worker voice_agent.app:app  8130 --workers 1
echo "launched. logs in /tmp/{stt-router,llm-router,tts-router,guardrail,voice-worker}.log"
