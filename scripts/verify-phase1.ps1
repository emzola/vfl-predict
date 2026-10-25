$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$goFiles = Get-ChildItem -Path $root -Recurse -Filter *.go | Where-Object { $_.FullName -notmatch '\\vendor\\' }
$unformatted = @(gofmt -l $goFiles.FullName)
if ($unformatted.Count -gt 0) {
    throw "Unformatted Go files: $($unformatted -join ', ')"
}

go vet ./...
go test -p 1 -count=1 ./...
python -m compileall -q services/model services
python -m unittest discover -s services/model/tests -p 'test_*.py'

$env:POSTGRES_PASSWORD = 'phase1-compose-placeholder'
docker compose -f infra/docker/docker-compose.yml config | Out-Null

if ($env:PHASE1_DATABASE_URL) {
    go test -count=1 ./services/ingest -run TestPostgresIntegrationIdempotencyAndInterruptedRecovery -v
} else {
    Write-Output 'PostgreSQL integration test skipped: set PHASE1_DATABASE_URL to a dedicated database.'
}

Write-Output 'Phase 1 verification passed.'
