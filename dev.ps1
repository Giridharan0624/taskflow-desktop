#
# dev.ps1 — launch TaskFlow Desktop in Wails hot-reload mode.
#
# Usage:
#   .\dev.ps1                # defaults to staging
#   .\dev.ps1 -Env staging
#   .\dev.ps1 -Env prod      # ONLY for final validation — never for iterative testing
#
# Copies config.<env>.json into config.json before launching so the app
# picks up the right API / Cognito values. config.json, config.staging.json
# and config.prod.json are all gitignored — update them locally per host.
#
param(
    [ValidateSet("staging", "prod")]
    [string]$Env = "staging"
)

$ErrorActionPreference = "Stop"

Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"

$envConfig = "config.$Env.json"
if (-not (Test-Path $envConfig)) {
    Write-Error "Missing $envConfig. Copy config.example.json to $envConfig and fill in the $Env values."
    exit 1
}

Copy-Item $envConfig "config.json" -Force
Write-Host "=== TaskFlow Desktop (dev) ===" -ForegroundColor Cyan
Write-Host "Environment: $Env" -ForegroundColor Yellow
$parsed = Get-Content "config.json" | ConvertFrom-Json
Write-Host "API URL:     $($parsed.api_url)"
Write-Host "Cognito:     $($parsed.cognito_user_pool_id)"
Write-Host ""

$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User") + ";C:\mingw64\bin"
$env:CGO_ENABLED = "1"
$env:CC = "gcc"
Write-Host "CGO_ENABLED=1 | GCC: $(gcc --version 2>&1 | Select-Object -First 1)"
Write-Host ""

wails dev
