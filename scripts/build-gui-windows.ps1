$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot

$version = if ($env:DUPLICA_SCAN_VERSION) { $env:DUPLICA_SCAN_VERSION } else { "dev" }

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    Write-Error "gcc not found in PATH. Add C:\msys64\mingw64\bin to PATH before building GUI."
}

$env:CGO_ENABLED = "1"
New-Item -ItemType Directory -Force -Path ".\bin" | Out-Null

# -H=windowsgui prevents opening a console window for the GUI executable.
go build -tags gui -ldflags "-H=windowsgui -X duplica-scan/src/internal/buildinfo.Version=$version" -o .\bin\duplica-scan-gui.exe .\src\cmd\duplica-scan-gui

Write-Host "Built GUI executable: .\bin\duplica-scan-gui.exe (version: $version)"
