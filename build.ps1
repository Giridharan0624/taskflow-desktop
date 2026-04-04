$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"

# ── Config values injected at build time (not stored on disk) ──
$pkg = "taskflow-desktop/internal/config"
$version = "1.0.0"
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
if ($LASTEXITCODE -eq 0) {
    $exe = Get-Item "build\bin\taskflow-desktop.exe" -ErrorAction SilentlyContinue
    Write-Host "BUILD SUCCESS - $([math]::Round($exe.Length / 1MB, 2)) MB"
} else {
    Write-Host "BUILD FAILED"
}
