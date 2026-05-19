# Capsy / Axcrio Platform — Operations Runbook

## Table of Contents

1. [Vault Secret Management](#vault-secret-management)
2. [JWT Secret Rotation](#jwt-secret-rotation)
3. [Local Development Setup](#local-development-setup)
4. [Service Health Checks](#service-health-checks)

---

## Vault Secret Management

All provider credentials and signing keys are stored in HashiCorp Vault KV v2
under the `secret/` mount. Services read from Vault when `VAULT_ADDR` is set
and fall back to environment variables automatically.

### Secret Paths

| Vault path                          | Contents                                      |
|-------------------------------------|-----------------------------------------------|
| `secret/data/capsy/jwt`             | `JWT_SECRET`, `JWT_REFRESH_SECRET`, `SESSION_SECRET` |
| `secret/data/capsy/hmac`            | `WEBHOOK_HMAC_SECRET`                         |
| `secret/data/capsy/providers/sarvam`    | `SARVAM_API_KEY`                              |
| `secret/data/capsy/providers/groq`      | `GROQ_API_KEY`                                |
| `secret/data/capsy/providers/openrouter`| `OPENROUTER_API_KEY`                          |
| `secret/data/capsy/providers/elevenlabs`| `ELEVENLABS_API_KEY`, `ELEVENLABS_VOICE_ID`   |
| `secret/data/capsy/providers/plivo`     | `PLIVO_AUTH_ID`, `PLIVO_AUTH_TOKEN`, `PLIVO_PHONE_NUMBER` |
| `secret/data/capsy/whatsapp/default`    | `META_WA_*` (all 5 fields)                    |
| `secret/data/capsy/providers/notification` | `POSTMARK_TOKEN`, `SLACK_WEBHOOK_URL`, `DISCORD_WEBHOOK_URL`, SES keys |
| `secret/data/capsy/providers/langfuse`  | `LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_HOST` |
| `secret/data/capsy/providers/qdrant`    | `QDRANT_URL`, `QDRANT_API_KEY`               |

### Starting Vault (local dev)

```powershell
docker run -d --name vault-dev -p 8200:8200 `
  -e VAULT_DEV_ROOT_TOKEN_ID=dev-root-token `
  -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 `
  hashicorp/vault:latest

$env:VAULT_ADDR  = "http://localhost:8200"
$env:VAULT_TOKEN = "dev-root-token"
```

### Seeding Secrets

Run after `pwsh scripts/load_credentials.ps1` has written `codebase/.env`:

```powershell
pwsh scripts/vault_seed.ps1
```

The script reads `codebase/.env`, generates random values for blank auto-generated
secrets, and writes all paths to Vault via the REST API. No vault CLI required.

### Rotating a Provider Key Manually

```powershell
# Read current value
vault kv get secret/capsy/providers/sarvam

# Write new value
vault kv put secret/capsy/providers/sarvam SARVAM_API_KEY=sk_new_value

# Services pick up the new value within 5 seconds (CachedKeyProvider TTL).
# After C3 (NATS), a jwt.secret.rotated event triggers immediate refresh.
```

---

## JWT Secret Rotation

JWT secrets are stored at `secret/data/capsy/jwt`. `tenant-auth` reads the
current key with a 5-second TTL cache. After rotation, the new key is active
within ≤5 seconds on all instances (after C3, a NATS event triggers immediate
refresh across all services).

### Rotate via API (recommended)

```bash
# Must be authenticated as super_admin
ACCESS_TOKEN=$(curl -s -XPOST http://localhost:8101/evs.v1.TenantAuthService/Login \
  -d '{"tenant_id":"default","email":"admin@axcrio.com","password":"<password>"}' \
  | jq -r .access_token)

curl -s -XPOST http://localhost:8101/v1/admin/rotate-jwt \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

Expected response:
```json
{"rotated": true, "hint": "new secret active within 5s on all instances (after C3 NATS wired)"}
```

### What happens after rotation

1. `RotateJWTSecret()` generates a 32-byte random hex string.
2. It is stored at `secret/data/capsy/jwt` key `JWT_SECRET` in Vault.
3. On the next `SigningKey()` call after the 5s TTL, the cache is refreshed.
4. All JWTs signed with the old secret are immediately invalid (they fail
   `jwt.Parse` since the signing key no longer matches).
5. Users must log in again to obtain a new access token.

### Rotate via Vault CLI (break-glass)

```bash
vault kv put secret/capsy/jwt JWT_SECRET=$(openssl rand -hex 32)
# Active within 5 seconds; no service restart needed.
```

---

## Local Development Setup

```powershell
# 1. Fill ALL_CREDENTIALS.md with your keys
# 2. Load credentials
pwsh scripts/load_credentials.ps1

# 3. Start dependencies
docker-compose up -d postgres redis vault-dev

# 4. Apply DB migrations
pwsh scripts/migrate_all.ps1

# 5. Seed Vault
$env:VAULT_ADDR = "http://localhost:8200"
$env:VAULT_TOKEN = "dev-root-token"
pwsh scripts/vault_seed.ps1

# 6. Start a service (example)
cd codebase/services/tenant-auth
$env:DATABASE_URL = "postgres://capsy:capsy@localhost:5432/capsy?sslmode=disable"
go run ./cmd/server
```

Services can run without Vault — they fall back to env vars automatically.

---

## Service Health Checks

| Service         | Port  | Health endpoint                                |
|----------------|-------|------------------------------------------------|
| tenant-auth     | 8101  | `GET http://localhost:8101/healthz`            |
| lead-import     | 8106  | `GET http://localhost:8106/healthz`            |
| whatsapp-adapter| 8119  | `GET http://localhost:8119/healthz`            |
| notification    | 8113  | `GET http://localhost:8113/healthz`            |
| campaign        | 8111  | `GET http://localhost:8111/healthz`            |
| bff             | 8080  | `GET http://localhost:8080/healthz`            |

Check all at once:

```powershell
@(8080,8101,8104,8105,8106,8107,8108,8109,8110,8111,8112,8113,8114,8115,8116,8117,8118,8119) | ForEach-Object {
    try {
        $r = Invoke-WebRequest "http://localhost:$_/healthz" -TimeoutSec 2 -ErrorAction Stop
        Write-Host "  [:$_] OK ($($r.StatusCode))"
    } catch {
        Write-Host "  [:$_] DOWN"
    }
}
```
