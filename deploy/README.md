# Deploy artifacts — box 139.59.89.18 (famit.in)

These mirror what is installed on the production box. Source of truth for
reboot survival, TLS, and reverse-proxy config.

## Reboot survival (systemd)
Both units are `enabled` so the stack comes back after a reboot:

- `systemd/voice-stack.service` → runs `droplet-start-all.sh`
  (telephony-adapter :8108 + STT/LLM/guardrail/TTS routers + voice worker :8130).
- `systemd/campaign-stack.service` → runs
  `codebase/scripts/droplet-start-campaign-stack.sh`
  (tenant-auth :8101, lead-import :8106, scheduler :8107, campaign :8115,
  bff :8090, analytics-sink :8116, call-intel :8119, dashboard :3000).
  Ordered `After=voice-stack.service`, waits 25s for docker infra (pg/redis/nats).

Install on box:
```
cp deploy/systemd/*.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable campaign-stack.service voice-stack.service
```

## nginx reverse proxy + TLS
- `nginx/voice.famit.in` → 443 → telephony-adapter :8108 (WebSocket upgrade,
  3600s timeouts). Cert issued via certbot (Let's Encrypt), auto-renew via the
  certbot systemd timer.
- `nginx/app.famit.in` → 80 → dashboard :3000 (HTTP only until DNS resolves).

### Finishing app.famit.in TLS
`app.famit.in` needs an A-record → 139.59.89.18 (add in GoDaddy). Once it
resolves, run on the box:
```
/root/finish-app-tls.sh
```
(`deploy/finish-app-tls.sh`) — it runs certbot --nginx for app.famit.in and
adds the HTTPS 443 server block + HTTP→HTTPS redirect, then verifies
`https://app.famit.in/login`.

## Secrets
The real `JWT_SECRET` / API keys live ONLY in `/root/lead/deploy-env.sh`
(chmod 600) on the box, never in this repo. `bff` and `tenant-auth` MUST share
the same `JWT_SECRET` or tenant-auth JWTs fail validation at the BFF.
