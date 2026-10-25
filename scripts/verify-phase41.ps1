param(
    [string]$DatabaseUrl = $env:PHASE41_DATABASE_URL,
    [string]$NpmCaFile = $env:NPM_CAFILE,
    [switch]$RunComposeSmoke,
    [switch]$KeepCompose,
    [string]$ComposeProject = 'bet9ja-vfl-phase41',
    [int]$ComposeWebPort = 3301,
    [int]$ComposePostgresPort = 55434
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $false)][string[]]$Arguments = @()
    )
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Executable $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

$webRoot = Join-Path $root 'apps/web'
if (-not (Test-Path -LiteralPath (Join-Path $webRoot 'package-lock.json'))) {
    throw 'apps/web/package-lock.json is required for reproducible frontend verification.'
}

$strictSSL = (& npm config get strict-ssl).Trim()
if ($LASTEXITCODE -ne 0 -or $strictSSL -ne 'true') {
    throw "npm strict-ssl must remain true; current value is '$strictSSL'."
}
Write-Output "Node: $((node --version).Trim())"
Write-Output "npm: $((npm --version).Trim())"
Write-Output "npm registry: $((npm config get registry).Trim())"
Write-Output "npm strict-ssl: $strictSSL"
if ($NpmCaFile) {
    if (-not (Test-Path -LiteralPath $NpmCaFile -PathType Leaf)) {
        throw "NpmCaFile does not exist: $NpmCaFile"
    }
    Write-Output "npm custom CA: $NpmCaFile"
} else {
    Write-Output 'npm custom CA: not configured; the system/Node trust store is used.'
}

Push-Location $webRoot
try {
    $npmCIArgs = @('ci', '--no-audit', '--no-fund')
    if ($NpmCaFile) {
        $npmCIArgs += "--cafile=$NpmCaFile"
    }
    Invoke-Checked 'npm' $npmCIArgs
    $npmAuditArgs = @('audit', '--audit-level=high')
    if ($NpmCaFile) {
        $npmAuditArgs += "--cafile=$NpmCaFile"
    }
    Invoke-Checked 'npm' $npmAuditArgs
    Invoke-Checked 'npm' @('test')
    Invoke-Checked 'npm' @('run', 'typecheck')
    Invoke-Checked 'npm' @('run', 'openapi:validate')
    Invoke-Checked 'npm' @('run', 'build')
} finally {
    Pop-Location
}

Invoke-Checked 'go' @('vet', './...')
Invoke-Checked 'go' @('test', '-p', '1', '-count=1', './...')

# Phase 0–4 offline/regression verification remains the compatibility gate.
& "$root/scripts/verify-phase4.ps1"
if ($LASTEXITCODE -ne 0) {
    throw "scripts/verify-phase4.ps1 failed with exit code $LASTEXITCODE"
}

$env:POSTGRES_PASSWORD = 'phase41-local-only'
$env:INTERNAL_API_TOKEN = 'phase41-internal-only'
$env:POSTGRES_DB = 'vfl_phase41'
$env:POSTGRES_USER = 'vfl'
$env:POSTGRES_HOST_PORT = "$ComposePostgresPort"
$env:WEB_HOST_PORT = "$ComposeWebPort"
$env:AUTHORIZED_EMAIL = 'owner@example.com'
$env:APP_BASE_URL = "http://127.0.0.1:$ComposeWebPort"
$env:MAILER_MODE = 'memory'

Invoke-Checked 'docker' @('compose', '-p', $ComposeProject, '-f', 'infra/docker/docker-compose.yml', 'config')

if ($DatabaseUrl) {
    $env:PHASE4_DATABASE_URL = $DatabaseUrl
    $env:PHASE4_ALLOW_MUTATING_TESTS = '1'
    Invoke-Checked 'go' @('test', '-p', '1', '-count=1', './services/api', '-run', 'TestPhase4(MigrationTables|AuthFlow|DashboardDataIntegration)$', '-v')
} else {
    Write-Output 'Phase 4.1 PostgreSQL integration skipped: pass -DatabaseUrl for a disposable database.'
}

if ($RunComposeSmoke) {
    $composeArgs = @('-p', $ComposeProject, '-f', 'infra/docker/docker-compose.yml')
    try {
        Invoke-Checked 'docker' (@('compose') + $composeArgs + @('up', '-d', '--build'))

        $postgresContainer = "$ComposeProject-postgres-1"
        $apiContainer = "$ComposeProject-api-1"
        $webContainer = "$ComposeProject-web-1"
        $healthy = $false
        for ($attempt = 0; $attempt -lt 60; $attempt++) {
            $postgresHealth = (& docker inspect --format '{{.State.Health.Status}}' $postgresContainer 2>$null).Trim()
            $apiHealth = (& docker inspect --format '{{.State.Health.Status}}' $apiContainer 2>$null).Trim()
            if ($postgresHealth -eq 'healthy' -and $apiHealth -eq 'healthy') {
                $healthy = $true
                break
            }
            Start-Sleep -Seconds 3
        }
        if (-not $healthy) {
            & docker compose @composeArgs ps
            throw 'Compose smoke test did not reach healthy PostgreSQL/API state within 180 seconds.'
        }

        $healthCode = (& curl.exe -sS -o NUL -w '%{http_code}' "http://127.0.0.1:$ComposeWebPort/api/v1/system/health").Trim()
        if ($healthCode -ne '200') {
            throw "Frontend/API health route returned HTTP $healthCode."
        }
        $dashboardCode = (& curl.exe -sS -o NUL -w '%{http_code}' "http://127.0.0.1:$ComposeWebPort/dashboard").Trim()
        if ($dashboardCode -ne '307' -and $dashboardCode -ne '308') {
            throw "Unauthenticated dashboard request returned HTTP $dashboardCode instead of a login redirect."
        }
        $apiPort = (& docker compose @composeArgs port api 8090).Trim()
        $postgresPort = (& docker compose @composeArgs port postgres 5432).Trim()
        if ($apiPort -and $apiPort -notmatch '^invalid IP:0$') {
            throw "Operational API unexpectedly publishes a host port: api='$apiPort'"
        }
        if ($postgresPort -and $postgresPort -notmatch '^127\.0\.0\.1:') {
            throw "PostgreSQL must remain loopback-only in local smoke: postgres='$postgresPort'"
        }

        Invoke-Checked 'docker' (@('compose') + $composeArgs + @('restart', 'api', 'web'))
        for ($attempt = 0; $attempt -lt 40; $attempt++) {
            $apiHealth = (& docker inspect --format '{{.State.Health.Status}}' $apiContainer 2>$null).Trim()
            if ($apiHealth -eq 'healthy') { break }
            Start-Sleep -Seconds 3
        }
        $healthCode = (& curl.exe -sS -o NUL -w '%{http_code}' "http://127.0.0.1:$ComposeWebPort/api/v1/system/health").Trim()
        if ($healthCode -ne '200') {
            throw "Health route did not recover after API/web restart; HTTP $healthCode."
        }
        Write-Output 'Private Compose smoke test passed: migrations, health, login redirect, private ports, and restart recovery.'
    } finally {
        if (-not $KeepCompose) {
            & docker compose @composeArgs down -v --remove-orphans
            if ($LASTEXITCODE -ne 0) {
                Write-Warning "Compose cleanup returned exit code $LASTEXITCODE"
            }
        }
    }
}

Write-Output 'Phase 4.1 verification passed for all requested executable gates.'
