$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"

Write-Host "=== Building TaskFlow Desktop Installer (COMPANY) ==="
Write-Host ""

# Step 1: Company build with injected config
# NOTE: Update these values after deploying the company stack:
#   cd backend/cdk && cdk deploy --app "python app_company.py"
#   Then copy ApiUrl, UserPoolId, UserPoolClientId from the outputs.
Write-Host "Step 1: Building company binary..."
$pkg = "taskflow-desktop/internal/config"
$version = "1.0.0"
$ldflags = @(
    "-X '${pkg}.apiURL=https://qhh92ze0rc.execute-api.ap-south-1.amazonaws.com/prod'"
    "-X '${pkg}.cognitoRegion=ap-south-1'"
    "-X '${pkg}.cognitoPoolID=ap-south-1_KvHp1RVEE'"
    "-X '${pkg}.cognitoClientID=7dakaniqm6vr19b7q165b8ppar'"
    "-X '${pkg}.webDashboardURL=https://taskflow.neurostack.in'"
    "-X 'taskflow-desktop/internal/updater.CurrentVersion=${version}'"
) -join " "

# Validate that placeholders have been replaced
if ($ldflags -match '<COMPANY_') {
    Write-Host ""
    Write-Host "ERROR: Update the placeholder values in this script first!"
    Write-Host "Deploy the company stack and fill in ApiUrl, UserPoolId, UserPoolClientId."
    Write-Host "  cd backend/cdk && cdk deploy --app 'python app_company.py'"
    exit 1
}

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
        Write-Host "COMPANY INSTALLER READY"
        Write-Host "File: $($installer.FullName)"
        Write-Host "Size: $([math]::Round($installer.Length / 1MB, 2)) MB"
        Write-Host ""
        Write-Host "Config:"
        Write-Host "  Dashboard: https://taskflow.neurostack.in"
    }
} else {
    Write-Host "NSIS build failed"
}
