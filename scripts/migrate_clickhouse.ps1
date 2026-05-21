param(
    [string]$ClickHouseUrl = $env:CLICKHOUSE_URL,
    [string]$MigrationsDir = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ClickHouseUrl)) {
    $ClickHouseUrl = "http://localhost:8123"
}

$headers = @{}
$builder = [System.UriBuilder]$ClickHouseUrl
if (-not [string]::IsNullOrWhiteSpace($builder.UserName)) {
    $pair = "$([System.Uri]::UnescapeDataString($builder.UserName)):$([System.Uri]::UnescapeDataString($builder.Password))"
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($pair)
    $headers["Authorization"] = "Basic " + [Convert]::ToBase64String($bytes)
    $builder.UserName = ""
    $builder.Password = ""
    $ClickHouseUrl = $builder.Uri.AbsoluteUri
}

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
if ([string]::IsNullOrWhiteSpace($MigrationsDir)) {
    $candidate = Join-Path $root "services\analytics-sink\migrations\clickhouse"
    if (Test-Path $candidate) {
        $MigrationsDir = $candidate
    } else {
        $MigrationsDir = Join-Path $root "infra\clickhouse"
    }
}

if (-not (Test-Path $MigrationsDir)) {
    throw "ClickHouse migrations directory not found: $MigrationsDir"
}

$endpoint = $ClickHouseUrl
$files = Get-ChildItem -Path $MigrationsDir -Filter "*.sql" -File | Sort-Object Name
if ($files.Count -eq 0) {
    throw "No ClickHouse .sql files found in $MigrationsDir"
}

foreach ($file in $files) {
    Write-Host "Applying ClickHouse migration $($file.Name)"
    $sql = Get-Content -LiteralPath $file.FullName -Raw
    $statements = [regex]::Split($sql, ";\s*(?:\r?\n|$)") |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    foreach ($statement in $statements) {
        Invoke-WebRequest `
            -Method Post `
            -Uri $endpoint `
            -Headers $headers `
            -ContentType "text/plain; charset=utf-8" `
            -Body $statement `
            -UseBasicParsing | Out-Null
    }
}

Write-Host "ClickHouse migrations applied from $MigrationsDir"
