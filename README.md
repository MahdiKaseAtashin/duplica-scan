# Duplica Scan

![Duplica Scan Logo](assets/duplica-scan-logo.png)

Cross-platform duplicate file scanner written in Go (Windows, macOS, Linux).

## Features

- Scan a drive or directory recursively.
- Detect duplicates by content hash (SHA-256).
- Group duplicates and display name, full path, and size.
- Stream file hashing in chunks to keep memory usage low.
- Use size pre-filtering to avoid unnecessary hashing.
- Show real-time scan and hash progress.
- Hash files concurrently with configurable worker limit.
- Export duplicate reports in CSV or JSON.
- Exclude files by extension, directories, and size range.
- Support automatic keep-newest/keep-oldest deletion selection.
- Review duplicate groups and select files to delete.
- Require explicit `YES` confirmation before deletion.
- Support dry run mode.

## Project Structure

```text
duplica-scan/
  src/
    cmd/duplica-scan/        # CLI entrypoint
    internal/model/          # shared data models
    internal/scanner/        # recursive file discovery
    internal/hash/           # chunk-based hashing
    internal/duplicates/     # duplicate detection engine
    internal/ui/             # console interaction and progress
    internal/cleanup/        # deletion workflow
  tests/                     # reserved for integration/e2e tests
  docs/                      # architecture notes and docs
```

## Cross-Platform Compatibility

- Uses only Go standard library packages.
- No operating system-specific APIs or external native dependencies.
- Works on Windows, macOS, and Linux with the same flags and behavior.

## Run

### Prerequisites

- Go 1.22+ installed and available in `PATH`

### Build (Windows PowerShell)

```powershell
go build -o .\bin\duplica-scan.exe .\src\cmd\duplica-scan
```

Build with version metadata:

```powershell
go build -ldflags "-X duplica-scan/src/internal/buildinfo.Version=0.0.1-dev" -o .\bin\duplica-scan.exe .\src\cmd\duplica-scan
.\bin\duplica-scan.exe -version
```

### Build (macOS/Linux Bash or Zsh)

```bash
mkdir -p ./bin
go build -o ./bin/duplica-scan ./src/cmd/duplica-scan
```

### Build GUI (Desktop App)

```bash
go build -tags gui -o ./bin/duplica-scan-gui ./src/cmd/duplica-scan-gui
```

On Windows, build with `-H=windowsgui` to prevent a console window:

```powershell
go build -tags gui -ldflags "-H=windowsgui" -o .\bin\duplica-scan-gui.exe .\src\cmd\duplica-scan-gui
```

Or run the helper script:

```powershell
.\scripts\build-gui-windows.ps1
```

Versioned release build (Windows CLI + GUI):

```powershell
$env:DUPLICA_SCAN_VERSION = "0.0.1-dev"
.\scripts\build-release-windows.ps1 -Version $env:DUPLICA_SCAN_VERSION
```

macOS helper script:

```bash
chmod +x ./scripts/build-gui-macos.sh
./scripts/build-gui-macos.sh
```

Linux helper script:

```bash
chmod +x ./scripts/build-gui-linux.sh
./scripts/build-gui-linux.sh
```

### Scan (Dry Run) - Windows

```powershell
.\bin\duplica-scan.exe -path "D:\Data" -dry-run=true -hash-workers=8
```

### Scan (Dry Run) - macOS/Linux

```bash
./bin/duplica-scan -path "/Users/you/Data" -dry-run=true -hash-workers=8
```

### Scan and Delete (Interactive) - Windows

```powershell
.\bin\duplica-scan.exe -path "D:\Data" -dry-run=false -hash-workers=8
```

### Scan and Delete (Interactive) - macOS/Linux

```bash
./bin/duplica-scan -path "/Users/you/Data" -dry-run=false -hash-workers=8
```

When prompted:
1. Enter comma-separated index numbers of files to delete in each group.
2. Type `YES` to confirm deletion.

### Worker Pool Tuning

- `-hash-workers` controls concurrent hashing.
- Default value is `runtime.NumCPU()`.
- Use lower values to reduce CPU/I/O pressure on busy systems.

### Export Reports

- `-export-format` supports `csv` or `json`.
- `-export-path` sets destination file path.
- If `-export-path` is omitted, the tool writes to `./reports/duplicate-report-<timestamp>.<ext>`.

Example:

```bash
./bin/duplica-scan -path "/Users/you/Data" -dry-run=true -hash-workers=8 -export-format=json
```

### Exclusion Filters

- `-exclude-exts` skips extensions (comma-separated), e.g. `.log,.tmp` or `log,tmp`.
- `-exclude-dirs` skips directory names (comma-separated), e.g. `node_modules,.git`.
- `-min-size-bytes` includes only files at or above this size.
- `-max-size-bytes` includes only files at or below this size.

Example:

```bash
./bin/duplica-scan -path "/Users/you/Data" -exclude-exts=".log,.tmp" -exclude-dirs="node_modules,.git" -min-size-bytes=1024 -max-size-bytes=104857600
```

### Auto Selection Strategies

- `-auto-select=newest` keeps the newest file in each duplicate group and selects the rest for deletion.
- `-auto-select=oldest` keeps the oldest file in each duplicate group and selects the rest for deletion.
- Safety confirmation is still required (`YES`) before deletion.

Example:

```bash
./bin/duplica-scan -path "/Users/you/Data" -dry-run=false -auto-select=newest
```

## GUI Wrapper

A small Fyne-based GUI is included for non-CLI users:

- Choose scan path using a folder picker.
- Configure hash workers, exclusions, min/max size.
- Optionally export CSV/JSON report.
- Optionally use auto-select strategy (`newest`/`oldest`) and confirm deletion via dialog.

Run:

```bash
./bin/duplica-scan-gui
```

### Windows GUI Prerequisites

Fyne desktop builds on Windows require CGO and a working C compiler toolchain.

1. Install MSYS2 and the `mingw-w64-x86_64-gcc` toolchain.
2. Add `C:\msys64\mingw64\bin` to `PATH`.
3. Enable CGO and build with the `gui` tag:

```powershell
$env:PATH = "C:\msys64\mingw64\bin;$env:PATH"
$env:CGO_ENABLED = "1"
gcc --version
go build -tags gui -o .\bin\duplica-scan-gui.exe .\src\cmd\duplica-scan-gui
```

If you see `build constraints exclude all Go files` for `go-gl`, verify `CGO_ENABLED=1` and that `gcc` resolves in `PATH`.

### macOS/Linux GUI Prerequisites

Fyne desktop builds on macOS/Linux also require CGO and native GUI/OpenGL development libraries.

- Ensure `CGO_ENABLED=1`.
- Ensure a C compiler is installed (`clang` on macOS, `gcc` on Linux).
- Install platform libraries typically required by GLFW/OpenGL builds.

Example checks:

```bash
go env CGO_ENABLED
cc --version
go build -tags gui -o ./bin/duplica-scan-gui ./src/cmd/duplica-scan-gui
```

On Linux, if build errors mention missing X11/OpenGL/GLFW headers, install your distro's development packages for OpenGL and X11/Wayland (for example `libgl1-mesa-dev`, `xorg-dev`, or equivalents).

Note: the GUI command is gated behind the `gui` build tag so CLI-only environments can still run `go test ./...` without desktop OpenGL toolchain requirements.

## Development

```bash
go test ./...
```
