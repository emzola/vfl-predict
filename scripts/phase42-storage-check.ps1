param(
    [string]$DatabaseUrl = $env:DATABASE_URL,
    [int]$WarningFreePercent = 25,
    [int]$CriticalFreePercent = 15,
    [int64]$MaximumWalBytes = 2147483648
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$driveName = (Get-Location).Drive.Name
$drive = Get-PSDrive -Name $driveName
$total = [int64]$drive.Used + [int64]$drive.Free
$freePercent = if ($total -gt 0) { [math]::Round(100 * $drive.Free / $total, 2) } else { 100 }
Write-Output "filesystem drive=$driveName total_bytes=$total free_bytes=$($drive.Free) free_percent=$freePercent"
if ($freePercent -lt $CriticalFreePercent) {
    throw "CRITICAL: filesystem free space is below $CriticalFreePercent%."
}
if ($freePercent -lt $WarningFreePercent) {
    Write-Warning "filesystem free space is below $WarningFreePercent%."
}

if (-not [string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    & "$root/scripts/phase42-storage-audit.ps1" -DatabaseUrl $DatabaseUrl -OutputDir 'artifacts/phase42-storage-check' -IncludeDocker
    if ($LASTEXITCODE -ne 0) {
        throw "Database storage check failed with exit code $LASTEXITCODE."
    }
    $audit = Get-Content (Join-Path $root 'artifacts/phase42-storage-check/database-audit.json') -Raw | ConvertFrom-Json
    if ($MaximumWalBytes -gt 0 -and [int64]$audit.database.wal.directory_bytes -gt $MaximumWalBytes) {
        throw "CRITICAL: PostgreSQL WAL directory exceeds $MaximumWalBytes bytes."
    }
} else {
    Write-Warning 'DATABASE_URL is not set; PostgreSQL metrics were not checked.'
}

if (Get-Command docker -ErrorAction SilentlyContinue) {
    docker system df
    docker ps -a --format 'table {{.Names}}\t{{.Status}}\t{{.Size}}'
    docker stats --no-stream --format 'container={{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}} memory_percent={{.MemPerc}}' 2>$null
} else {
    Write-Warning 'Docker CLI is not available; Docker storage was not checked.'
}
