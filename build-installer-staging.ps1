$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User") + ";C:\mingw64\bin"
$env:CGO_ENABLED = "1"
$env:CC = "gcc"
Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"

Write-Host "=== Building TaskFlow Desktop Installer (STAGING) ==="
Write-Host ""

# Step 1: Staging build with injected config
Write-Host "Step 1: Building staging binary..."
$pkg = "taskflow-desktop/internal/config"
$version = "1.0.0-staging"
$ldflags = @(
    "-X '${pkg}.apiURL=https://mcx0iyvisf.execute-api.ap-south-1.amazonaws.com/prod'"
    "-X '${pkg}.cognitoRegion=ap-south-1'"
    "-X '${pkg}.cognitoPoolID=ap-south-1_yWxQYrYXp'"
    "-X '${pkg}.cognitoClientID=6eaa6ej7a3j1p5jm5ooq1ui0g3'"
    "-X '${pkg}.webDashboardURL=http://localhost:3000'"
    "-X 'taskflow-desktop/internal/updater.CurrentVersion=${version}'"
) -join " "
wails build -ldflags "$ldflags" 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "BUILD FAILED"
    exit 1
}

$exe = Get-Item "build\bin\taskflow-desktop.exe" -ErrorAction SilentlyContinue
Write-Host "Binary: $($exe.FullName) ($([math]::Round($exe.Length / 1MB, 2)) MB)"

# Step 2: Check for NSIS
Write-Host ""
Write-Host "Step 2: Checking NSIS..."
$nsis = Get-Command makensis -ErrorAction SilentlyContinue
if (-not $nsis) {
    $nsisPath = "C:\Program Files (x86)\NSIS\makensis.exe"
    if (Test-Path $nsisPath) {
        $nsis = Get-Item $nsisPath
        Write-Host "Found NSIS at $nsisPath"
    } else {
        Write-Host "NSIS not found. Install it from https://nsis.sourceforge.io/Download"
        Write-Host "Then re-run this script."
        exit 1
    }
}

# Step 3: Build installer
Write-Host ""
Write-Host "Step 3: Building installer..."
Set-Location "build\windows\installer"
& $nsis.FullName project.nsi 2>&1
if ($LASTEXITCODE -eq 0) {
    $installer = Get-Item "TaskFlowDesktop-Setup-1.0.0.exe" -ErrorAction SilentlyContinue
    if ($installer) {
        Write-Host ""
        Write-Host "STAGING INSTALLER READY"
        Write-Host "File: $($installer.FullName)"
        Write-Host "Size: $([math]::Round($installer.Length / 1MB, 2)) MB"
        Write-Host ""
        Write-Host "Config:"
        Write-Host "  API:       https://mcx0iyvisf.execute-api.ap-south-1.amazonaws.com/prod"
        Write-Host "  Pool:      ap-south-1_yWxQYrYXp"
        Write-Host "  Dashboard: http://localhost:3000"
    }
} else {
    Write-Host "NSIS build failed"
}
