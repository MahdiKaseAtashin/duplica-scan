# Regenerates assets/windows/app.ico from assets/duplica-scan-logo.png (Windows .exe icon resource).
# Requires Windows PowerShell with System.Drawing (desktop .NET Framework).

$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$src = Join-Path $projectRoot "assets\duplica-scan-logo.png"
$dir = Join-Path $projectRoot "assets\windows"
$dst = Join-Path $dir "app.ico"

if (-not (Test-Path $src)) {
    Write-Error "Missing source PNG: $src"
}

New-Item -ItemType Directory -Force -Path $dir | Out-Null
Add-Type -AssemblyName System.Drawing

$img = [System.Drawing.Image]::FromFile((Resolve-Path $src))
$size = [Math]::Min(256, [Math]::Max($img.Width, $img.Height))
$bmp = New-Object System.Drawing.Bitmap([int]$size, [int]$size)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.Clear([System.Drawing.Color]::FromArgb(0, 0, 0, 0))
$g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
$g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
$g.DrawImage($img, 0, 0, [float]$size, [float]$size)
$g.Dispose()

$icon = [System.Drawing.Icon]::FromHandle($bmp.GetHicon())
$fs = [System.IO.File]::Create($dst)
$icon.Save($fs)
$fs.Close()
$img.Dispose()
$bmp.Dispose()

Write-Host "Wrote $dst"
