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
go run ./cmd/phase2-verify
python -m compileall -q services/model services
python -m unittest discover -s services/model/tests -p 'test_*.py'

$env:POSTGRES_PASSWORD = 'phase2-compose-placeholder'
docker compose -f infra/docker/docker-compose.yml config | Out-Null

if ($env:PHASE2_DATABASE_URL) {
    go test -p 1 -count=1 ./services/research -run TestPhase2DatabaseSchemaAndResearchStorage -v
} else {
    Write-Output 'Phase 2 PostgreSQL integration test skipped: set PHASE2_DATABASE_URL to a dedicated database.'
}

Write-Output 'Phase 2 verification passed.'
