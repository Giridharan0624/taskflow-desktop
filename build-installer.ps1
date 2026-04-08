$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User") + ";C:\mingw64\bin"
$env:CGO_ENABLED = "1"
$env:CC = "gcc"
Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"

# ── Config ──
$pkg = "taskflow-desktop/internal/config"
$version = "1.0.0"

Write-Host "=== Building TaskFlow Desktop Installer v${version} ==="
Write-Host "CGO_ENABLED=1 | GCC: $(gcc --version 2>&1 | Select-Object -First 1)"
Write-Host ""

# ── Step 1: Production build ──
Write-Host "Step 1: Building production binary..."
$ldflags = @(
    "-X '${pkg}.apiURL=https://3syc4x99a7.execute-api.ap-south-1.amazonaws.com/prod'"
    "-X '${pkg}.cognitoRegion=ap-south-1'"
    "-X '${pkg}.cognitoPoolID=ap-south-1_72qWKeSH5'"
    "-X '${pkg}.cognitoClientID=pentcto4cmlfof93tsv738nct'"
    "-X '${pkg}.webDashboardURL=https://taskflow-ns.vercel.app'"
    "-X 'taskflow-desktop/internal/updater.CurrentVersion=${version}'"
) -join " "

go mod tidy 2>&1
wails build -ldflags "$ldflags" 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "BUILD FAILED"
    exit 1
}

$exe = Get-Item "build\bin\taskflow-desktop.exe" -ErrorAction SilentlyContinue
Write-Host "Binary: $($exe.FullName) ($([math]::Round($exe.Length / 1MB, 2)) MB)"

# ── Step 2: Check for NSIS ──
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

# ── Step 3: Build installer ──
Write-Host ""
Write-Host "Step 3: Building installer..."
Set-Location "build\windows\installer"
& $nsis.FullName project.nsi 2>&1
if ($LASTEXITCODE -eq 0) {
    $installer = Get-Item "TaskFlowDesktop-Setup-${version}.exe" -ErrorAction SilentlyContinue
    if ($installer) {
        Write-Host ""
        Write-Host "=== INSTALLER READY ==="
        Write-Host "File: $($installer.FullName)"
        Write-Host "Size: $([math]::Round($installer.Length / 1MB, 2)) MB"
        Write-Host "Version: v${version}"
    }
} else {
    Write-Host "NSIS build failed"
    exit 1
}
