$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User") + ";C:\mingw64\bin"
$env:CGO_ENABLED = "1"
$env:CC = "gcc"
Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"

# ── Config values injected at build time (not stored on disk) ──
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

Write-Host "=== Building TaskFlow Desktop v${version} ==="
Write-Host "CGO_ENABLED=1 | GCC: $(gcc --version 2>&1 | Select-Object -First 1)"
Write-Host ""

go mod tidy 2>&1
wails build -ldflags "$ldflags" 2>&1
if ($LASTEXITCODE -eq 0) {
    $exe = Get-Item "build\bin\taskflow-desktop.exe" -ErrorAction SilentlyContinue
    Write-Host ""
    Write-Host "BUILD SUCCESS"
    Write-Host "Output: $($exe.FullName)"
    Write-Host "Size: $([math]::Round($exe.Length / 1MB, 2)) MB"
    Write-Host "Version: v${version}"
} else {
    Write-Host "BUILD FAILED"
    exit 1
}
