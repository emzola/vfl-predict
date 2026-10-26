$ErrorActionPreference = 'Stop'

Write-Host 'Phase 6 deterministic verification'
foreach ($path in @(
  'scripts/auto-promote-phase6-production.sh',
  'scripts/promote-phase6-production.sh',
  'scripts/activate-phase6-production.sh',
  'db/migrations/012_phase6_dynamic_bayesian.sql'
)) {
  if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "missing Phase 6 artifact: $path" }
}
go test -count=1 ./services/research ./services/prediction ./services/api
go vet ./services/research ./services/prediction ./services/api ./cmd/phase6-evaluate ./cmd/phase6-shadow ./cmd/phase6-promote ./cmd/phase6-register ./cmd/phase6-approve ./cmd/phase6-activate
go run ./cmd/phase6-verify

Write-Host 'Phase 6 frontend verification'
Push-Location apps/web
try {
  npm run typecheck
  npm test
} finally {
  Pop-Location
}

Write-Host 'Phase 6 verification passed'
