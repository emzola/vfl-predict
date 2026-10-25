param(
    [string]$DatabaseUrl = $env:DATABASE_URL,
    [string]$BackupDir = 'backups/postgres'
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw 'Set DATABASE_URL through the environment or pass -DatabaseUrl.'
}
if (-not (Get-Command pg_dump -ErrorAction SilentlyContinue)) {
    throw 'pg_dump is required; use the PostgreSQL client tools or the Linux runbook.'
}

$root = Split-Path -Parent $PSScriptRoot
$absoluteBackupDir = Join-Path $root $BackupDir
New-Item -ItemType Directory -Force -Path $absoluteBackupDir | Out-Null
$timestamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$backupPath = Join-Path $absoluteBackupDir "vfl-$timestamp.dump"
& pg_dump --format=custom --no-owner --no-privileges --dbname=$DatabaseUrl --file=$backupPath
if ($LASTEXITCODE -ne 0) { throw "pg_dump failed with exit code $LASTEXITCODE." }
& pg_restore --list $backupPath | Out-Null
if ($LASTEXITCODE -ne 0) { throw "pg_restore verification failed with exit code $LASTEXITCODE." }
Write-Output "logical backup verified: $backupPath"
