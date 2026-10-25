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

$env:POSTGRES_PASSWORD = 'phase3-compose-placeholder'
docker compose -f infra/docker/docker-compose.yml config | Out-Null

if ($env:PHASE3_DATABASE_URL) {
    go test -p 1 -count=1 ./services/ingest ./services/research ./services/prediction ./services/settlement -run 'Test(PostgresIntegrationIdempotencyAndInterruptedRecovery|Phase2DatabaseSchemaAndResearchStorage|Phase3DatabaseSchemaAndFrozenGuard|Phase3PredictionGenerationIsIdempotent|Phase3ForwardSettlementIntegration)' -v
} else {
    Write-Output 'Phase 3 PostgreSQL integration test skipped: set PHASE3_DATABASE_URL to a dedicated database.'
}

Write-Output 'Phase 3 verification passed.'
