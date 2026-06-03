# LiveKit self-hosted voice stack (localhost-only unit)

Self-hosted LiveKit voice stack — Redis, `livekit-server`, and `livekit/sip` —
deployed via Docker Compose on the hardened box. **This unit keeps everything
bound to localhost.** No new public port is opened. The box stays externally
closed except SSH/22. Opening allowlisted SIP/media ports is a later unit.

## Security posture

- Every published host port binds **`127.0.0.1` only** (never `0.0.0.0`):
  - `127.0.0.1:6379` → redis
  - `127.0.0.1:7880` → livekit-server HTTP/WS API
- WebRTC UDP mux (`7882`), SIP signaling (`5060`), and the RTP range
  (`10000-20000`) are configured but **NOT published to the host** in this unit.
  They are container-internal only.
- UFW remains default-deny inbound with **only SSH/22 allowed** — untouched by
  this deploy.
- Secrets (`LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, `REDIS_PASSWORD`) live ONLY
  in `/opt/livekit/.env` on the box (`chmod 600`). They are **never committed**.
  The files in this directory contain `${ENV_VAR}` placeholders only.

## Files (committed — placeholders only, no secrets)

| File                 | Purpose |
|----------------------|---------|
| `docker-compose.yml` | Three services; host ports bound to 127.0.0.1 only. |
| `livekit.yaml`       | livekit-server structural config (ports/rtc/logging). Keys + redis creds come from env at runtime. |
| `sip.yaml`           | SIP config **template** with `${ENV_VAR}` placeholders. |
| `README.md`          | This file. |

## On-box files (NOT committed)

| File                       | Notes |
|----------------------------|-------|
| `/opt/livekit/.env`        | Real secrets, `chmod 600`, owned by `famit`. |
| `/opt/livekit/sip.runtime.yaml` | Rendered from `sip.yaml` via `envsubst`, `chmod 600`. The `livekit/sip` binary does **not** expand `${ENV_VAR}` inside its YAML, so the runtime copy holds the substituted values and is mounted into the sip container. |

## Networking model

A private compose bridge network (`livekit`). Containers reach each other by
service name (`redis:6379`, `ws://livekit-server:7880`). Host-facing ports are
published bound to `127.0.0.1` so the services are reachable from the box itself
(loopback) but never from any external interface. `livekit-server` receives its
API keys and redis credentials via native env vars (`LIVEKIT_KEYS`,
`REDIS_HOST`, `REDIS_PASSWORD`), keeping secrets out of the mounted YAML.

## Deploy / operate (on the box)

```bash
cd /opt/livekit

# .env must exist (chmod 600) with LIVEKIT_API_KEY / LIVEKIT_API_SECRET / REDIS_PASSWORD.

# Render the SIP runtime config from the committed template:
set -a; . /opt/livekit/.env; set +a
envsubst < /opt/livekit/sip.yaml > /opt/livekit/sip.runtime.yaml
chmod 600 /opt/livekit/sip.runtime.yaml

docker compose up -d
docker compose ps
```

### Verify localhost-only bindings

```bash
# Only :22 should be a non-loopback listener; 6379/7880 must show 127.0.0.1 only.
sudo ss -ltnp | grep -E '127.0.0.1:6379|127.0.0.1:7880|:22'

# Server health (loopback):
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7880/   # -> 200

# UFW must remain only-22:
sudo ufw status verbose
```

## Images deployed

- `redis:7`
- `livekit/livekit-server:v1.8`
- `livekit/sip:latest`

## Next unit (out of scope here)

Open and allowlist the SIP signaling port (5060) and the RTP/media range to
permitted peers only, publish the WebRTC UDP mux port, and add the corresponding
UFW allow rules scoped to known SIP trunk / media source IPs.
