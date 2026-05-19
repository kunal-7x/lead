param(
    [string]$DatabaseUrl = $env:DATABASE_URL,
    [string]$DockerContainer = $env:POSTGRES_CONTAINER
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    $DatabaseUrl = "postgres://capsy:capsy@localhost:5432/capsy?sslmode=disable"
}

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
$services = @(
    "tenant-auth",
    "lead-import",
    "lead-identity",
    "consent-compliance",
    "knowledge",
    "campaign",
    "scheduler",
    "telephony-adapter",
    "whatsapp-adapter",
    "handoff",
    "notification",
    "site-visit",
    "billing-meter",
    "analytics-sink",
    "model-config",
    "internal-admin-api",
    "freeswitch-bridge"
)

function Get-ServiceSchemas([string]$Service) {
    $schema = $Service.Replace("-", "_")
    if ($Service -eq "tenant-auth") {
        return @($schema, "public")
    }
    return @($schema)
}

function Get-UpSql([string]$Path) {
    $lines = Get-Content -Path $Path
    if (-not ($lines | Where-Object { $_ -match '^\s*--\s*\+goose\s+Up\s*$' })) {
        return ($lines -join [Environment]::NewLine)
    }

    $out = New-Object System.Collections.Generic.List[string]
    $inUp = $false
    foreach ($line in $lines) {
        if ($line -match '^\s*--\s*\+goose\s+Up\s*$') {
            $inUp = $true
            continue
        }
        if ($line -match '^\s*--\s*\+goose\s+Down\s*$') {
            $inUp = $false
            continue
        }
        if ($inUp) {
            $out.Add($line)
        }
    }
    return ($out -join [Environment]::NewLine)
}

function Get-SqlLiteral([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

function Get-Identifier([string]$Value) {
    return '"' + $Value.Replace('"', '""') + '"'
}

$psqlCommand = Get-Command psql -ErrorAction SilentlyContinue

if (-not $psqlCommand) {
    $runningContainers = @(docker ps --format "{{.Names}}")
    if ([string]::IsNullOrWhiteSpace($DockerContainer)) {
        if ($runningContainers -contains "pg-capsy") {
            $DockerContainer = "pg-capsy"
        } elseif ($runningContainers -contains "pg-test") {
            $DockerContainer = "pg-test"
        }
    }
    if ([string]::IsNullOrWhiteSpace($DockerContainer)) {
        throw "psql is not on PATH and no running pg-capsy/pg-test container was found."
    }
}

function Invoke-PSQL([string]$Sql) {
    if ($psqlCommand) {
        $Sql | & $psqlCommand.Source $DatabaseUrl -v ON_ERROR_STOP=1
    } else {
        $Sql | docker exec -i $DockerContainer psql $DatabaseUrl -v ON_ERROR_STOP=1
    }
    if ($LASTEXITCODE -ne 0) {
        throw "psql failed with exit code $LASTEXITCODE"
    }
}

function Invoke-PSQLScalar([string]$Sql) {
    if ($psqlCommand) {
        $out = & $psqlCommand.Source $DatabaseUrl -At -v ON_ERROR_STOP=1 -c $Sql
    } else {
        $out = docker exec -i $DockerContainer psql $DatabaseUrl -At -v ON_ERROR_STOP=1 -c $Sql
    }
    if ($LASTEXITCODE -ne 0) {
        throw "psql scalar failed with exit code $LASTEXITCODE"
    }
    return (($out | Select-Object -First 1) -as [string]).Trim()
}

function Ensure-LocalCapsyDatabase {
    if ($psqlCommand -or [string]::IsNullOrWhiteSpace($DockerContainer)) {
        return
    }
    if ($DatabaseUrl -notmatch "capsy:capsy@localhost") {
        return
    }

    $adminSql = @"
DO `$`$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'capsy') THEN
        CREATE ROLE capsy LOGIN PASSWORD 'capsy' CREATEROLE;
    ELSE
        ALTER ROLE capsy CREATEROLE;
    END IF;
END
`$`$;
SELECT 'CREATE DATABASE capsy OWNER capsy'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'capsy')\gexec
"@
    $adminSql | docker exec -i $DockerContainer psql -U postgres -d postgres -v ON_ERROR_STOP=1
    if ($LASTEXITCODE -ne 0) {
        throw "failed to ensure local capsy database in $DockerContainer"
    }
}

Ensure-LocalCapsyDatabase

Invoke-PSQL @"
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE IF NOT EXISTS public.schema_migrations (
    service TEXT NOT NULL,
    migration TEXT NOT NULL,
    checksum TEXT,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service, migration)
);
"@

foreach ($service in $services) {
    foreach ($schema in (Get-ServiceSchemas $service)) {
        $schemaIdent = Get-Identifier $schema
        $indexIdent = Get-Identifier ("idx_" + $schema + "_c1_store_records_tenant")

        Invoke-PSQL @"
CREATE SCHEMA IF NOT EXISTS $schemaIdent;
CREATE TABLE IF NOT EXISTS $schemaIdent.c1_store_records (
    kind TEXT NOT NULL,
    key TEXT NOT NULL,
    tenant_id TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, key)
);
CREATE INDEX IF NOT EXISTS $indexIdent
    ON $schemaIdent.c1_store_records (kind, tenant_id, created_at);
"@

        $migrationDir = Join-Path $root "services\$service\migrations"
        if (-not (Test-Path $migrationDir)) {
            continue
        }

        Get-ChildItem -Path $migrationDir -Filter "*.sql" | Sort-Object Name | ForEach-Object {
            $migration = $_.Name
            $migrationService = "$service@$schema"
            $applied = Invoke-PSQLScalar ("SELECT 1 FROM public.schema_migrations WHERE service = " + (Get-SqlLiteral $migrationService) + " AND migration = " + (Get-SqlLiteral $migration) + " LIMIT 1;")
            if ($applied -eq "1") {
                Write-Host "skip $service/$migration into schema $schema"
                return
            }

            Write-Host "apply $service/$migration into schema $schema"
            $body = Get-UpSql $_.FullName
            $sql = @"
SET search_path TO $schemaIdent, public;
$body
INSERT INTO public.schema_migrations (service, migration, checksum)
VALUES ($(Get-SqlLiteral $migrationService), $(Get-SqlLiteral $migration), NULL)
ON CONFLICT (service, migration) DO NOTHING;
"@
            Invoke-PSQL $sql
        }
    }
}

Write-Host "migrate_all complete"
