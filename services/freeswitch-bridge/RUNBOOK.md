# freeswitch-bridge Runbook

Purpose: bridge FreeSWITCH ESL events and `mod_audio_stream` WebSocket audio into the Capsy voice-agent-worker.

## Local C12 Smoke Test

1. Start local infra:

```powershell
cd codebase
docker compose -f infra/docker-compose.dev.yml up -d nats redis
```

2. Start the voice agent:

```powershell
cd codebase\services\voice-agent-worker
uv run uvicorn voice_agent.app:app --host 0.0.0.0 --port 8200
```

3. Start the bridge:

```powershell
cd codebase\services\freeswitch-bridge
$env:NATS_URL = "nats://localhost:4222"
$env:VOICE_AGENT_WS_URL = "ws://localhost:8200/ws/audio"
$env:FREESWITCH_HOST = "127.0.0.1"
$env:FREESWITCH_ESL_PORT = "8021"
$env:ESL_PASSWORD = "<value from ALL_CREDENTIALS.md>"
go run .\cmd\server
```

4. Prepare a temporary full FreeSWITCH config.

The public `signalwire/freeswitch:1.10` image was not pullable in this environment. Use `safarov/freeswitch:latest`, which expects config at `/etc/freeswitch`. Start from its vanilla config, then overlay the Capsy C12 files.

```powershell
cd codebase
$env:ESL_PASSWORD = "<value from ALL_CREDENTIALS.md>"
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$fsConf = "C:\tmp\capsy-c12\fsconf-$stamp"
New-Item -ItemType Directory -Path $fsConf | Out-Null
docker run --rm -v "${fsConf}:/out" --entrypoint sh safarov/freeswitch:latest -c "cp -a /usr/share/freeswitch/conf/vanilla/. /out/"
Copy-Item infra\freeswitch\conf\autoload_configs\event_socket.conf.xml "$fsConf\autoload_configs\event_socket.conf.xml" -Force
Copy-Item infra\freeswitch\conf\autoload_configs\modules.conf.xml "$fsConf\autoload_configs\modules.conf.xml" -Force
Copy-Item infra\freeswitch\conf\sip_profiles\external.xml "$fsConf\sip_profiles\external.xml" -Force
Copy-Item infra\freeswitch\conf\dialplan\default.xml "$fsConf\dialplan\default.xml" -Force
(Get-Content "$fsConf\autoload_configs\event_socket.conf.xml" -Raw).Replace('${ESL_PASSWORD}', $env:ESL_PASSWORD).Replace('${ESL_APPLY_INBOUND_ACL}', 'any_v4.auto') | Set-Content "$fsConf\autoload_configs\event_socket.conf.xml" -NoNewline
((Get-Content "$fsConf\autoload_configs\modules.conf.xml" -Raw) -replace '(?m)^\s*<load module="mod_audio_stream"/>\s*\r?\n?', '') | Set-Content "$fsConf\autoload_configs\modules.conf.xml" -NoNewline
```

5. Run FreeSWITCH for fast local ESL/SIP proof:

```powershell
docker run -d --name capsy-freeswitch-c12 `
  -p 5060:5060/udp -p 5060:5060/tcp -p 8021:8021 `
  -e SOUND_RATES= `
  -e SOUND_TYPES= `
  -e VOICE_AGENT_WS_URL=ws://host.docker.internal:8109/ws/audio `
  -v "${fsConf}:/etc/freeswitch" `
  safarov/freeswitch:latest
```

6. Verify ESL:

```powershell
docker exec capsy-freeswitch-c12 fs_cli -p $env:ESL_PASSWORD -x "status"
```

7. Generate a local channel event without a SIP softphone:

```powershell
docker exec capsy-freeswitch-c12 fs_cli -p $env:ESL_PASSWORD -x "load mod_loopback"
```

In one terminal, subscribe to NATS:

```powershell
docker run --rm --network container:nats natsio/nats-box:latest nats sub --server nats://127.0.0.1:4222 "freeswitch.>" --count 1 --timeout 30s
```

In another terminal, originate and clean up the local test call:

```powershell
docker exec capsy-freeswitch-c12 fs_cli -p $env:ESL_PASSWORD -x "originate {originate_timeout=3,tenant_id=local,session_id=c12-final-nats,provider_call_id=c12-final-nats}null/9196 &park()"
docker exec capsy-freeswitch-c12 fs_cli -p $env:ESL_PASSWORD -x "hupall NORMAL_CLEARING"
```

Expected behavior:

- FreeSWITCH starts and `fs_cli status` returns ready.
- `freeswitch-bridge` opens an established ESL connection to `127.0.0.1:8021`.
- NATS receives `freeswitch.channel.created` with tenant/session/provider fields.
- The WebSocket proxy forwards binary L16 PCM frames to `voice-agent-worker` at `/ws/audio/{session_id}` and converts playback JSON back to `streamAudio` frames for FreeSWITCH.

Do not use the custom FreeSWITCH source Docker build as the C12 acceptance gate. It is kept as an optional later artifact for full `mod_audio_stream` image proof.

## Deferred Carrier Test

Real PSTN proof remains blocked until Exotel, Jio, or Plivo credentials exist. Carrier adapters should point FreeSWITCH to the same dialplan variables: `tenant_id`, `session_id`, `caller_id_number`, and `provider_call_id`.

## Operations

Paging signals: ESL disconnect loops, call answer without `mod_audio_stream::connect`, high audio frame loss, bridge restarts, recording upload queue growth.

Common fixes: verify FreeSWITCH container health, confirm `ESL_PASSWORD`, check `VOICE_AGENT_WS_URL`, inspect NATS for `freeswitch.*`, and fail calls over to mock/demo mode if audio is unstable.

Rollback: deploy previous bridge image and disable new call launch if the audio path is unstable.
