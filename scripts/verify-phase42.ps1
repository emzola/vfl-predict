param(
    [string]$DatabaseUrl = $env:PHASE42_DATABASE_URL,
    [switch]$IncludeDocker
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    Write-Output 'Phase 4.2 storage-audit unit tests'
    go test -p 1 -count=1 ./services/storageaudit ./cmd/phase42-storage-audit ./cmd/phase42-retention-dry-run
    if ($LASTEXITCODE -ne 0) { throw "storage audit tests failed with exit code $LASTEXITCODE." }

    $migration = Get-Content db/migrations/010_phase42_storage_policy.sql -Raw
    foreach ($required in @('storage_retention_policies', 'storage_audit_snapshots', 'autovacuum_vacuum_scale_factor', '010_phase42_storage_policy')) {
        if ($migration -notmatch [regex]::Escape($required)) {
            throw "Phase 4.2 migration is missing required marker: $required"
        }
    }
    foreach ($script in @(
        'scripts/phase42-storage-audit.ps1',
        'scripts/phase42-retention-dry-run.ps1',
        'scripts/phase42-storage-check.ps1',
        'scripts/postgres-logical-backup.ps1'
    )) {
        if (-not (Test-Path $script)) { throw "missing Phase 4.2 script: $script" }
    }
    if ((Get-Content scripts/postgres-logical-backup.sh -Raw) -notmatch 'pg_dump --format=custom') {
        throw 'logical backup script does not use custom pg_dump format'
    }
    if ((Get-Content scripts/postgres-logical-restore.sh -Raw) -notmatch 'CONFIRM_RESTORE=YES') {
        throw 'logical restore script is not explicitly guarded'
    }
    Write-Output 'Phase 4.2 static checks passed.'

    if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
        Write-Output 'Phase 4.2 PostgreSQL checks skipped: pass -DatabaseUrl or set PHASE42_DATABASE_URL.'
    } else {
        if ($IncludeDocker) {
            & "$root/scripts/phase42-storage-audit.ps1" -DatabaseUrl $DatabaseUrl -OutputDir 'artifacts/phase42-verification' -IncludeDocker
        } else {
            & "$root/scripts/phase42-storage-audit.ps1" -DatabaseUrl $DatabaseUrl -OutputDir 'artifacts/phase42-verification'
        }
        & "$root/scripts/phase42-retention-dry-run.ps1" -DatabaseUrl $DatabaseUrl -OutputPath 'artifacts/phase42-verification/retention.json'
        if ($LASTEXITCODE -ne 0) { throw 'Phase 4.2 PostgreSQL verification failed.' }
        Write-Output 'Phase 4.2 PostgreSQL audit and retention dry-run passed.'
    }
} finally {
    Pop-Location
}
