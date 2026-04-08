$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User") + ";C:\mingw64\bin"
$env:CGO_ENABLED = "1"
$env:CC = "gcc"
Set-Location "D:\NEUROSTACK\PROJECTS\task-management\desktop"
Write-Host "Starting TaskFlow Desktop in dev mode (hot reload)..."
Write-Host "CGO_ENABLED=1 | GCC: $(gcc --version 2>&1 | Select-Object -First 1)"
Write-Host ""
wails dev
