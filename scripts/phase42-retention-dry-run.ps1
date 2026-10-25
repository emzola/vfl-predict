param(
    [string]$DatabaseUrl = $env:DATABASE_URL,
    [string]$OutputPath = 'artifacts/phase42-retention-dry-run.json'
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw 'Set DATABASE_URL or pass -DatabaseUrl. This command never deletes or archives rows.'
}

$absoluteOutput = Join-Path $root $OutputPath
$parent = Split-Path -Parent $absoluteOutput
New-Item -ItemType Directory -Force -Path $parent | Out-Null
$previousDatabaseUrl = $env:DATABASE_URL
$env:DATABASE_URL = $DatabaseUrl
try {
    & go run ./cmd/phase42-retention-dry-run -output $absoluteOutput
    if ($LASTEXITCODE -ne 0) {
        throw "Phase 4.2 retention dry-run failed with exit code $LASTEXITCODE."
    }
} finally {
    if ($null -eq $previousDatabaseUrl) {
        Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
    } else {
        $env:DATABASE_URL = $previousDatabaseUrl
    }
}
Write-Output "Retention dry-run written to $absoluteOutput"
