# Embeds assets/windows/app.ico into Windows PE resources (rsrc.syso) for CLI and GUI builds.
# Run before go build on Windows; commit app.ico only - rsrc.syso is gitignored.

$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$ico = Join-Path $projectRoot "assets\windows\app.ico"

if (-not (Test-Path $ico)) {
    Write-Error "Missing $ico - run scripts/generate-app-ico.ps1 first."
}

Set-Location $projectRoot

$rsrcMod = "github.com/akavel/rsrc@v0.10.2"
go run $rsrcMod -arch amd64 -ico $ico -o "src\cmd\duplica-scan-gui\rsrc.syso"
go run $rsrcMod -arch amd64 -ico $ico -o "src\cmd\duplica-scan\rsrc.syso"

Write-Host "Embedded icon: src/cmd/duplica-scan-gui/rsrc.syso, src/cmd/duplica-scan/rsrc.syso"
