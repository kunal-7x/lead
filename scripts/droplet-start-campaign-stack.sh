#!/usr/bin/env bash
# Start the C13 campaign stack on the droplet WITHOUT touching the voice mesh.
# Voice mesh owns 8108/8110/8111/8112/8113/8130 -> we never bind those.
# Campaign stack ports: tenant-auth 8101, lead-import 8106, scheduler 8107,
#                       campaign 8115, bff 8090, dashboard 3000.
set -e
export PATH=$PATH:/usr/local/go/bin:/root/.local/bin
ROOT=/root/lead/codebase
LOGS=/root/lead/logs
BIN=/root/lead/bin
mkdir -p "$LOGS" "$BIN"

# Load base env, then overlay (DATABASE_URL=evs, NATS, REDIS, etc).
. /root/lead/droplet-loadenv.sh

# ---- campaign-stack port + wiring overrides (do NOT collide with voice mesh) ----
export PORT=8101                                   # tenant-auth reads PORT
export TENANT_AUTH_ADDR=:8101
export LEAD_IMPORT_ADDR=:8106
export SCHEDULER_ADDR=:8107
export CAMPAIGN_ADDR=:8115                         # NOT :8112 (guardrail) / :8111 (llm)
export BFF_ADDR=:8090
# bff -> downstream service URLs
export TENANT_AUTH_URL=http://localhost:8101
export LEAD_IMPORT_URL=http://localhost:8106
export SCHEDULER_URL=http://localhost:8107
export CAMPAIGN_URL=http://localhost:8115
export REDIS_ADDR=localhost:6379
export ALLOWED_ORIGIN=${ALLOWED_ORIGIN:-http://139.59.23.204:3000}
# scheduler dispatcher wiring
export TELEPHONY_URL=http://localhost:8108
export LEAD_IMPORT_URL=http://localhost:8106
export CAMPAIGN_URL=http://localhost:8115
export LLM_ROUTER_URL=http://localhost:8111
export FROM_NUMBER=${FROM_NUMBER:-+918071583488}
# campaign preflight off for live (matches prior live stack)
export CAMPAIGN_PREFLIGHT_DISABLED=1

start_go () {
  local name="$1" pkg="$2" port="$3"
  echo "killing stale $name on :$port"
  fuser -k "${port}/tcp" 2>/dev/null || true
  sleep 0.3
  echo "building $name"
  (cd "$ROOT" && go build -o "$BIN/$name" "$pkg")
  echo "starting $name on :$port"
  nohup "$BIN/$name" > "$LOGS/$name.log" 2>&1 &
  echo $! > "$LOGS/$name.pid"
  sleep 0.5
}

start_go tenant-auth ./services/tenant-auth/cmd/server 8101
start_go lead-import ./services/lead-import/cmd/server 8106
start_go campaign    ./services/campaign/cmd/server    8115
start_go scheduler   ./services/scheduler/cmd/server   8107
start_go bff         ./services/bff/cmd/bff             8090

echo "CAMPAIGN_STACK_STARTED"
