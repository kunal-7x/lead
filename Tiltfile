# EVS local dev stack
# Run: tilt up
# Requires: Docker, tilt (https://tilt.dev), kubectl (local cluster optional)

load('ext://helm_resource', 'helm_resource')

# ---------- Infrastructure Services ----------

docker_compose('./infra/docker-compose.dev.yml')

dc_resource('postgres',  labels=['infra'])
dc_resource('redis',     labels=['infra'])
dc_resource('nats',      labels=['infra'])
dc_resource('temporal',  labels=['infra'])
dc_resource('livekit',   labels=['infra'])
dc_resource('vault',     labels=['infra'])
