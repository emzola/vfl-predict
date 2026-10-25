$ErrorActionPreference = 'Stop'

$formatted = gofmt -l services/provider
if ($formatted) {
    throw "Go files require gofmt: $formatted"
}

go vet ./...
go test ./...
python -m compileall -q services/model
python -m unittest discover -s services/model/tests -p 'test_*.py'

if (-not $env:POSTGRES_PASSWORD) {
    $env:POSTGRES_PASSWORD = 'ci-placeholder-only'
}
docker compose -f infra/docker/docker-compose.yml config --quiet
Write-Output 'Phase 0 verification passed.'
