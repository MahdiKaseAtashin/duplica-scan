$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot

$version = if ($env:CLEANPULSE_VERSION) { $env:CLEANPULSE_VERSION } else { "dev" }

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    Write-Error "gcc not found in PATH. Add C:\msys64\mingw64\bin to PATH before building GUI."
}

& "$PSScriptRoot\embed-windows-icon.ps1"

$env:CGO_ENABLED = "1"
New-Item -ItemType Directory -Force -Path ".\bin" | Out-Null

# -H=windowsgui prevents opening a console window for the GUI executable.
go build -tags gui -ldflags "-H=windowsgui -X cleanpulse/src/internal/buildinfo.Version=$version" -o .\bin\cleanpulse-gui.exe .\src\cmd\cleanpulse-gui

Write-Host "Built GUI executable: .\bin\cleanpulse-gui.exe (version: $version)"
