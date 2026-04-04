$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"

# ── Config values injected at build time (not stored on disk) ──
$pkg = "taskflow-desktop/internal/config"
$version = "1.0.0"
$ldflags = @(
    "-X '${pkg}.apiURL=https://4saz9agwdi.execute-api.ap-south-1.amazonaws.com/staging'"
    "-X '${pkg}.cognitoRegion=ap-south-1'"
    "-X '${pkg}.cognitoPoolID=ap-south-1_NedaPlHsx'"
    "-X '${pkg}.cognitoClientID=36i0ejo32b4c5u6un0g75h4bme'"
    "-X '${pkg}.webDashboardURL=https://taskflow-ns.vercel.app'"
    "-X 'taskflow-desktop/internal/updater.CurrentVersion=${version}'"
) -join " "

go mod tidy 2>&1
wails build -ldflags "$ldflags" 2>&1
if ($LASTEXITCODE -eq 0) {
    $exe = Get-Item "build\bin\taskflow-desktop.exe" -ErrorAction SilentlyContinue
    Write-Host "BUILD SUCCESS - $([math]::Round($exe.Length / 1MB, 2)) MB"
} else {
    Write-Host "BUILD FAILED"
}
