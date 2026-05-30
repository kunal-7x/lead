#!/usr/bin/env bash
# Apply tenant-auth goose-style SQL migrations into the evs DB on the droplet.
# Other C13 services (campaign/scheduler/lead-import) use pgkv auto-create on boot.
# Idempotent: tracks applied migrations in public.schema_migrations.
set -euo pipefail

PGC="${POSTGRES_CONTAINER:-infra-postgres-1}"
DB="${PGDB:-evs}"
USER="${PGUSER:-evs}"
ROOT="${CODEBASE_ROOT:-/root/lead/codebase}"

psql() { docker exec -i "$PGC" psql -U "$USER" -d "$DB" -v ON_ERROR_STOP=1 "$@"; }

# pgcrypto for gen_random_uuid + migration ledger
psql <<'SQL'
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE IF NOT EXISTS public.schema_migrations (
    service TEXT NOT NULL,
    migration TEXT NOT NULL,
    checksum TEXT,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service, migration)
);
SQL

apply_service() {
  local service="$1" schema="$2"
  psql -c "CREATE SCHEMA IF NOT EXISTS \"$schema\";"
  local dir="$ROOT/services/$service/migrations"
  [ -d "$dir" ] || { echo "no migrations for $service"; return 0; }
  local svckey="$service@$schema"
  for f in $(ls "$dir"/*.sql | sort); do
    local name; name="$(basename "$f")"
    local applied
    applied="$(psql -At -c "SELECT 1 FROM public.schema_migrations WHERE service='$svckey' AND migration='$name' LIMIT 1;")"
    if [ "$applied" = "1" ]; then echo "skip $service/$name"; continue; fi
    echo "apply $service/$name -> $schema"
    # extract +goose Up section (between '-- +goose Up' and '-- +goose Down')
    local up; up="$(awk '/^[[:space:]]*--[[:space:]]*\+goose[[:space:]]+Up[[:space:]]*$/{f=1;next} /^[[:space:]]*--[[:space:]]*\+goose[[:space:]]+Down[[:space:]]*$/{f=0} f' "$f")"
    if [ -z "$up" ]; then up="$(cat "$f")"; fi
    printf 'SET search_path TO "%s", public;\n%s\nINSERT INTO public.schema_migrations(service,migration) VALUES('"'"'%s'"'"','"'"'%s'"'"') ON CONFLICT DO NOTHING;\n' "$schema" "$up" "$svckey" "$name" | psql
  done
}

apply_service tenant-auth tenant_auth
echo "migrate_droplet complete"
