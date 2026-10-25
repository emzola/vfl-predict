param(
    [string]$DatabaseUrl = $env:PHASE36_DATABASE_URL,
    [switch]$RunLiveCycle,
    [switch]$ReclaimExpiredLease,
    [int]$RequestSpacingSeconds = 10
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if ($RequestSpacingSeconds -lt 10) {
    throw 'Phase 3.6 requires at least 10 seconds between upstream requests.'
}

if ($DatabaseUrl) {
    $env:PHASE1_DATABASE_URL = $DatabaseUrl
    $env:PHASE2_DATABASE_URL = $DatabaseUrl
    $env:PHASE3_DATABASE_URL = $DatabaseUrl
    $env:PHASE35_DATABASE_URL = $DatabaseUrl
    $env:PHASE36_DATABASE_URL = $DatabaseUrl
}

$goFiles = Get-ChildItem -Path $root -Recurse -Filter *.go | Where-Object { $_.FullName -notmatch '\\vendor\\' }
$unformatted = @(gofmt -l $goFiles.FullName)
if ($unformatted.Count -gt 0) {
    throw "Unformatted Go files: $($unformatted -join ', ')"
}

go vet ./...
go test -p 1 -count=1 ./...
go run ./cmd/phase1-verify
go run ./cmd/phase2-verify
go run ./cmd/phase3-verify
go run ./cmd/phase35-verify
go run ./cmd/phase36-verify

python -m compileall -q services/model services
python -m unittest discover -s services/model/tests -p 'test_*.py'

$env:POSTGRES_PASSWORD = 'phase36-compose-placeholder'
$env:INTERNAL_API_TOKEN = 'phase36-compose-internal-placeholder'
docker compose -f infra/docker/docker-compose.yml config | Out-Null

if ($DatabaseUrl) {
    go test -p 1 -count=1 ./services/ingest -run 'TestPhase36LeaseOwnershipAndExpiredRecovery' -v
    if ($ReclaimExpiredLease) {
        go run ./cmd/phase36-lease -database-url $DatabaseUrl -reclaim-expired
    } else {
        go run ./cmd/phase36-lease -database-url $DatabaseUrl
    }
    go run ./cmd/phase36-report -database-url $DatabaseUrl -report tmp/phase36-forward-report.json
    go run ./cmd/phase36-verify -database-url $DatabaseUrl -report tmp/phase36-forward-report.json
} else {
    Write-Output 'Phase 3.6 PostgreSQL checks skipped: set PHASE36_DATABASE_URL or pass -DatabaseUrl.'
}

if ($RunLiveCycle) {
    if (-not $DatabaseUrl) {
        throw '-RunLiveCycle requires -DatabaseUrl or PHASE36_DATABASE_URL.'
    }
    $env:DATABASE_URL = $DatabaseUrl
    $env:BET9JA_MAX_RETRIES = '0'
    go run ./cmd/live-monitor -database-url $DatabaseUrl -allow-network -run-once -request-spacing-seconds $RequestSpacingSeconds
    go run ./cmd/phase36-report -database-url $DatabaseUrl -report tmp/phase36-forward-report.json
} else {
    Write-Output 'No provider request was made. Use -RunLiveCycle only after confirming authorization and lease state.'
}

Write-Output 'Phase 3.6 verification passed.'
