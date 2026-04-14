param(
    [string]$Version = "0.0.1-dev"
)

$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    Write-Error "gcc not found in PATH. Add C:\msys64\mingw64\bin to PATH before release build."
}

& "$PSScriptRoot\embed-windows-icon.ps1"

$env:CGO_ENABLED = "1"
New-Item -ItemType Directory -Force -Path ".\bin\release" | Out-Null

$ldflagsCli = "-X cleanpulse/src/internal/buildinfo.Version=$Version"
$ldflagsGui = "-H=windowsgui -X cleanpulse/src/internal/buildinfo.Version=$Version"

go build -ldflags $ldflagsCli -o ".\bin\release\cleanpulse-$Version-windows-amd64.exe" .\src\cmd\cleanpulse
go build -tags gui -ldflags $ldflagsGui -o ".\bin\release\cleanpulse-gui-$Version-windows-amd64.exe" .\src\cmd\cleanpulse-gui

Write-Host "Built release artifacts in .\bin\release for version $Version"
