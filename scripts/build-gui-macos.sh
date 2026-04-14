#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

VERSION="${CLEANPULSE_VERSION:-dev}"

if ! command -v cc >/dev/null 2>&1; then
  echo "error: C compiler not found. Install Xcode Command Line Tools (xcode-select --install)." >&2
  exit 1
fi

export CGO_ENABLED=1
mkdir -p ./bin

go build -tags gui -ldflags "-X cleanpulse/src/internal/buildinfo.Version=${VERSION}" -o ./bin/cleanpulse-gui ./src/cmd/cleanpulse-gui
echo "Built GUI executable: ./bin/cleanpulse-gui (version: ${VERSION})"
