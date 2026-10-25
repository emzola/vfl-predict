param(
    [switch]$AllowNetwork,
    [string]$DatabaseUrl = $env:DATABASE_URL,
    [int]$RequestSpacingSeconds = 10
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not $AllowNetwork) {
    throw 'This script is intentionally offline by default. Re-run with -AllowNetwork after confirming provider authorization.'
}
if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw 'Set DATABASE_URL or pass -DatabaseUrl for a dedicated development database.'
}
if ($RequestSpacingSeconds -lt 10) {
    throw 'Phase 3.5 live validation requires at least 10 seconds between provider requests.'
}

$env:DATABASE_URL = $DatabaseUrl
$env:BET9JA_MAX_RETRIES = '0'
go run ./cmd/live-monitor -database-url $DatabaseUrl -allow-network -run-once -request-spacing-seconds $RequestSpacingSeconds

Write-Output 'The cycle above is a bounded observation only. Inspect live_feed_observations, odds_change_history, round_states, prediction_runs, and job_execution_history before treating any result as forward evidence.'
Write-Output 'A held live-monitor lease is not force-released by this script; resolve the active worker or wait for its TTL.'
