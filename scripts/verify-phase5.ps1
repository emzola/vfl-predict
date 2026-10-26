param(
    [string]$NpmCaFile = $env:NPM_CAFILE,
    [switch]$RunBrowserE2E,
    [switch]$RunPriorOfflineVerifiers
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $false)][string[]]$Arguments = @(),
        [string]$WorkingDirectory = $root
    )
    Push-Location $WorkingDirectory
    try {
        & $Executable @Arguments
        if ($LASTEXITCODE -ne 0) {
            throw "$Executable $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
}

Write-Output 'Phase 5 static deployment checks'
foreach ($path in @(
    '.github/workflows/phase5-production.yml',
    '.github/workflows/rollback-production.yml',
    'infra/docker/docker-compose.production.yml',
    'infra/docker/Caddyfile.production',
    'infra/docker/.env.production.example',
    'infra/docker/Dockerfile.api',
    'infra/docker/Dockerfile.worker',
    'scripts/bootstrap-vps.sh',
    'scripts/deploy-production.sh',
    'scripts/rollback-production.sh',
    'scripts/promote-phase6-production.sh',
    'scripts/auto-promote-phase6-production.sh',
    'scripts/activate-phase6-production.sh',
    'scripts/production-health-report.sh',
    'docs/phase-5-deployment.md'
)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "missing Phase 5 artifact: $path"
    }
}

$compose = Get-Content infra/docker/docker-compose.production.yml -Raw
foreach ($required in @('postgres-data', 'migrate', 'phase6-research', '1536m', 'RUN_MIGRATIONS', 'caddy', '80:80', '443:443', 'no-new-privileges', 'max-size', 'mem_limit', 'service_completed_successfully')) {
    if ($compose -notmatch [regex]::Escape($required)) { throw "production Compose missing required marker: $required" }
}
if ($compose -match 'ports:\s*\r?\n\s*-\s*"?\d+:5432') { throw 'production Compose publishes PostgreSQL to the host' }
if ($compose -match 'down\s+-v') { throw 'destructive Compose volume removal found' }

$caddy = Get-Content infra/docker/Caddyfile.production -Raw
foreach ($required in @('reverse_proxy api:8090', 'reverse_proxy web:3000', 'replace token REDACTED', 'headers.Cookie delete', 'Cache-Control "no-store"')) {
    if ($caddy -notmatch [regex]::Escape($required)) { throw "Caddy configuration missing required marker: $required" }
}

$deploy = Get-Content scripts/deploy-production.sh -Raw
foreach ($required in @('flock', 'image-digests.txt', 'compose pull migrate phase6-research', 'compose run --rm --no-deps migrate', 'current-release', 'previous-release')) {
    if ($deploy -notmatch [regex]::Escape($required)) { throw "deployment script missing required marker: $required" }
}
if ($deploy -match 'docker\s+compose[^\r\n]*down\s+-v') { throw 'deployment script contains docker compose down -v' }

$phase6Promotion = Get-Content scripts/promote-phase6-production.sh -Raw
foreach ($required in @('CONFIRM_PROMOTION', 'phase6-promote', 'prediction-worker', 'model-promotion.lock', 'PHASE6_EVALUATION_REPORT', 'PHASE6_SHADOW_REPORT')) {
    if ($phase6Promotion -notmatch [regex]::Escape($required)) { throw "Phase 6 promotion script missing required marker: $required" }
}
if ($phase6Promotion -match 'docker\s+compose[^\r\n]*down\s+-v') { throw 'Phase 6 promotion script contains docker compose down -v' }
$phase6AutomaticPromotion = Get-Content scripts/auto-promote-phase6-production.sh -Raw
foreach ($required in @('phase6-evaluate', 'phase6-shadow', 'phase6-register', 'phase6-approve', 'phase6-research', 'bootstrap-replicates 100', 'promote-phase6-production.sh', 'pre-cutoff shadow')) {
    if ($phase6AutomaticPromotion -notmatch [regex]::Escape($required)) { throw "automatic Phase 6 promotion script missing required marker: $required" }
}
if ($phase6AutomaticPromotion -match 'docker\s+compose[^\r\n]*down\s+-v') { throw 'automatic Phase 6 promotion script contains docker compose down -v' }
$phase6DirectActivation = Get-Content scripts/activate-phase6-production.sh -Raw
foreach ($required in @('CONFIRM_ACTIVATION', 'phase6-activate', 'model-promotion.lock', 'PREDICTION_MODEL=dynamic_bayesian_team_strength', 'prediction-worker', 'without replay or shadow evaluation')) {
    if ($phase6DirectActivation -notmatch [regex]::Escape($required)) { throw "direct Phase 6 activation script missing required marker: $required" }
}
if ($phase6DirectActivation -match 'docker\s+compose[^\r\n]*down\s+-v') { throw 'direct Phase 6 activation script contains docker compose down -v' }
$workflow = Get-Content .github/workflows/phase5-production.yml -Raw
foreach ($required in @('Activate Phase 6 production model without replay', 'scripts/activate-phase6-production.sh')) {
    if ($workflow -notmatch [regex]::Escape($required)) { throw "production workflow missing automatic Phase 6 marker: $required" }
}
if ($workflow -match 'Run automatic Phase 6 replay, shadow, and promotion gates') { throw 'production workflow still runs automatic Phase 6 replay gates' }

$nativePreference = $PSNativeCommandUseErrorActionPreference
$PSNativeCommandUseErrorActionPreference = $false
$trackedSecrets = & git grep -n -I -E 'BEGIN (RSA|OPENSSH|EC|DSA) PRIVATE KEY|ghp_[A-Za-z0-9]{20,}|re_[A-Za-z0-9]{30,}' -- ':!*.example' ':!docs/phase-*'
$grepExitCode = $LASTEXITCODE
$PSNativeCommandUseErrorActionPreference = $nativePreference
if ($grepExitCode -eq 0 -and $trackedSecrets) { throw "possible credential material found in tracked files: $trackedSecrets" }

Write-Output 'Compose interpolation check'
$composeEnv = @{
    RELEASE_SHA = '0123456789abcdef0123456789abcdef01234567'
    GHCR_IMAGE_PREFIX = 'ghcr.io/example/example'
    POSTGRES_PASSWORD = 'ci-placeholder-only'
    INTERNAL_API_TOKEN = 'ci-internal-placeholder'
    AUTHORIZED_EMAIL = 'owner@example.test'
    RESEND_API_KEY = 're_ci_placeholder'
    EMAIL_FROM = 'VFL <no-reply@example.test>'
    ACME_EMAIL = 'owner@example.test'
    BET9JA_HISTORICAL_BASE_URL = 'https://example.test/historical'
    BET9JA_LIVE_BASE_URL = 'https://example.test/live'
}
$previousEnvironment = @{}
foreach ($name in $composeEnv.Keys) { $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name); [Environment]::SetEnvironmentVariable($name, $composeEnv[$name], 'Process') }
try {
    Invoke-Checked 'docker' @('compose', '--env-file', 'infra/docker/.env.production.example', '-f', 'infra/docker/docker-compose.production.yml', 'config', '--quiet')
} finally {
    foreach ($name in $composeEnv.Keys) { [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process') }
}

if (-not (Test-Path apps/web/package-lock.json)) { throw 'apps/web/package-lock.json is required' }
$strictSSL = (& npm config get strict-ssl).Trim()
if ($strictSSL -ne 'true') { throw "npm strict-ssl must remain true; current value is '$strictSSL'" }
$npmArgs = @('ci', '--no-fund')
if ($NpmCaFile) { $npmArgs += "--cafile=$NpmCaFile" }
Invoke-Checked 'npm' $npmArgs (Join-Path $root 'apps/web')
$auditArgs = @('audit', '--audit-level=high')
if ($NpmCaFile) { $auditArgs += "--cafile=$NpmCaFile" }
Invoke-Checked 'npm' $auditArgs (Join-Path $root 'apps/web')
Invoke-Checked 'npm' @('test') (Join-Path $root 'apps/web')
Invoke-Checked 'npm' @('run', 'typecheck') (Join-Path $root 'apps/web')
Invoke-Checked 'npm' @('run', 'openapi:validate') (Join-Path $root 'apps/web')
Invoke-Checked 'npm' @('run', 'build') (Join-Path $root 'apps/web')
if ($RunBrowserE2E) {
    Invoke-Checked 'npx' @('playwright', 'install', 'chromium') (Join-Path $root 'apps/web')
    Invoke-Checked 'npm' @('run', 'test:e2e') (Join-Path $root 'apps/web')
} else {
    Write-Output 'Browser E2E skipped: pass -RunBrowserE2E after installing Chromium.'
}

Invoke-Checked 'go' @('vet', './...')
Invoke-Checked 'go' @('test', '-p', '1', '-count=1', './...')
$gofmtFiles = & gofmt -l cmd services
if ($LASTEXITCODE -ne 0) { throw 'gofmt check failed.' }
if ($gofmtFiles) { throw "gofmt required for: $($gofmtFiles -join ', ')" }

if ($RunPriorOfflineVerifiers) {
    & "$root/scripts/verify-phase4.ps1"
    if ($LASTEXITCODE -ne 0) { throw 'Phase 4 offline verifier failed.' }
    & "$root/scripts/verify-phase42.ps1"
    if ($LASTEXITCODE -ne 0) { throw 'Phase 4.2 offline verifier failed.' }
}

Write-Output 'Phase 5 deterministic verification passed.'
