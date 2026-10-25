param(
    [string]$DatabaseUrl = $env:DATABASE_URL,
    [string]$OutputDir = 'artifacts/phase42-storage-audit',
    [switch]$IncludeDocker
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw 'Set DATABASE_URL or pass -DatabaseUrl. This command performs read-only PostgreSQL inspection.'
}

$outputPath = Join-Path $root $OutputDir
New-Item -ItemType Directory -Force -Path $outputPath | Out-Null
$jsonPath = Join-Path $outputPath 'database-audit.json'
$markdownPath = Join-Path $outputPath 'database-audit.md'
$arguments = @(
    'run', './cmd/phase42-storage-audit',
    '-output', $jsonPath,
    '-markdown', $markdownPath
)
if ($IncludeDocker) { $arguments += '-include-docker' }
$previousDatabaseUrl = $env:DATABASE_URL
$env:DATABASE_URL = $DatabaseUrl
try {
    & go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Phase 4.2 storage audit failed with exit code $LASTEXITCODE."
    }
} finally {
    if ($null -eq $previousDatabaseUrl) {
        Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
    } else {
        $env:DATABASE_URL = $previousDatabaseUrl
    }
}
Write-Output "Read-only storage audit written to $jsonPath and $markdownPath"
