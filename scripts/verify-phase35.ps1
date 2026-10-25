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
go run ./cmd/phase1-verify
go run ./cmd/phase2-verify
go run ./cmd/phase3-verify
go run ./cmd/phase35-verify

python -m compileall -q services/model services
python -m unittest discover -s services/model/tests -p 'test_*.py'

$env:POSTGRES_PASSWORD = 'phase35-compose-placeholder'
docker compose -f infra/docker/docker-compose.yml config | Out-Null

if ($env:PHASE35_DATABASE_URL) {
    go test -p 1 -count=1 ./services/research -run 'TestPhase35ModelRegistryIntegration' -v
    go run ./cmd/phase35-verify -database-url $env:PHASE35_DATABASE_URL
} else {
    Write-Output 'Phase 3.5 PostgreSQL integration test skipped: set PHASE35_DATABASE_URL to a dedicated database.'
}

Write-Output 'Phase 3.5 verification passed.'
