# Chatwoot — white-labeled self-hosted (Phase 3, famit.in / Axcrio)

Self-hosted Chatwoot deployed on a dedicated DigitalOcean droplet, white-labeled to
the Axcrio brand. Customer-facing domain: **famit.in** (Chatwoot UI at
`chat.famit.in`). This dir holds the deploy config (sanitized — no secrets).

## Droplet
- Name: `famit-chatwoot`  | DO Droplet ID: `574353896`
- IP: `168.144.94.20`     | Region: `blr1` (Bangalore) | Size: `s-2vcpu-4gb` (~$24/mo)
- Image: `ubuntu-24-04-x64` | SSH key: DO key id `56622232` (`do-blr-test`)
- 2GB swap enabled (`/swapfile`, `vm.swappiness=10`) — mandatory; Chatwoot OOMs on 4GB w/o it.
- UFW: 22, 80, 443, 3000 open. (Port 3000 is temporary; will move behind 443/TLS after DNS.)
- Deploy path on droplet: `/opt/chatwoot/`
- SSH: `ssh -i C:\Users\kunal\.ssh\do-blr-test\id_ed25519 root@168.144.94.20`

## Stack (Docker Compose, official production setup)
- Image: `chatwoot/chatwoot:v4.14.1` (pinned)
- Services: `rails`, `sidekiq`, `postgres` (pgvector/pgvector:pg16), `redis` (redis:alpine)
- Healthcheck: `curl http://168.144.94.20:3000/api` → `{"version":"4.14.1", ... "ok"}`

## Files here
- `docker-compose.yaml` — exact compose deployed (no secrets).
- `.env.template` — full env with all secret VALUES blanked. The real `.env` lives
  ONLY on the droplet at `/opt/chatwoot/.env` (chmod 600). Generate secrets with
  `openssl rand -hex 64` (SECRET_KEY_BASE) / `openssl rand -hex 24` (passwords).

## White-label applied
- **Tier 1 (env):** INSTALLATION_NAME=Axcrio, BRAND_NAME=Axcrio, BRAND_URL=https://famit.in,
  FRONTEND_URL=https://chat.famit.in, WIDGET_BRAND_URL, TERMS_URL, PRIVACY_URL,
  MAILER_SENDER_EMAIL, LOGO/LOGO_DARK/LOGO_THUMBNAIL placeholders.
- **Tier 2 (super-admin / installation_configs in DB):** INSTALLATION_NAME, BRAND_NAME,
  BRAND_URL, WIDGET_BRAND_URL, TERMS_URL, PRIVACY_URL set. Brand color + logo/favicon
  upload = human task via `/super_admin` (see ../../need.md).
- **Tier 3 (source fork to remove widget "Powered by Chatwoot"):** DEFERRED. Requires
  forking + rebuilding the image. MIT license permits full rebrand.
- License: Chatwoot core is MIT — full rebrand permitted.

## Super-admin
- Login URL: `http://168.144.94.20:3000/super_admin` (later `https://chat.famit.in/super_admin`)
- Email: `axcrio.inc@gmail.com` (User id=1, type=SuperAdmin).
- Password: recorded in repo-root `ALL_CREDENTIALS.md` §11 (NOT in this repo file).

## Redeploy from scratch
```bash
ssh root@168.144.94.20
cd /opt/chatwoot
cp .env.template .env            # then fill SECRET_KEY_BASE + 3 passwords
docker compose pull
docker compose run --rm rails bundle exec rails db:chatwoot_prepare
docker compose up -d
```

## Pending (human / post-DNS) — see repo-root need.md
- GoDaddy DNS A-records for famit.in.
- After DNS live: Claude runs certbot/TLS, sets FORCE_SSL=true, locks port 3000.
- SMTP creds, brand logo/favicon files, Meta WhatsApp Business API token.
