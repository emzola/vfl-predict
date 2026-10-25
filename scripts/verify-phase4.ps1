param(
    [string]$DatabaseUrl = $env:PHASE4_DATABASE_URL
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if ($DatabaseUrl) {
    $env:PHASE1_DATABASE_URL = $DatabaseUrl
    $env:PHASE2_DATABASE_URL = $DatabaseUrl
    $env:PHASE3_DATABASE_URL = $DatabaseUrl
    $env:PHASE35_DATABASE_URL = $DatabaseUrl
    $env:PHASE36_DATABASE_URL = $DatabaseUrl
    $env:PHASE4_DATABASE_URL = $DatabaseUrl
}

& "$root/scripts/verify-phase36.ps1" -DatabaseUrl $DatabaseUrl
go test -p 1 -count=1 ./services/api -run 'Test(NormalizeEmailAndRedirectValidation|AuthCookiesAreOpaqueAndSecurelyScoped|ProtectedDashboardRequiresSession|LoginLinkFailsClosedWhenDatabaseUnavailable|Phase4AuthMigrationTables)' -v

$env:POSTGRES_PASSWORD = 'phase4-compose-placeholder'
$env:INTERNAL_API_TOKEN = 'phase4-compose-internal-placeholder'
docker compose -f infra/docker/docker-compose.yml config | Out-Null

$webRoot = Join-Path $root 'apps/web'
if ((Test-Path (Join-Path $webRoot 'package-lock.json')) -and (Test-Path (Join-Path $webRoot 'node_modules'))) {
    Push-Location $webRoot
    try {
        npm test
        npm run typecheck
        npm run build
    } finally {
        Pop-Location
    }
} else {
    Write-Output 'Frontend verification skipped: npm package-lock.json/node_modules is unavailable in this environment.'
}

Write-Output 'Phase 4 verification passed for all executable checks.'
