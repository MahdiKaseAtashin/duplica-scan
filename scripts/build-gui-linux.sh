#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

if ! command -v gcc >/dev/null 2>&1; then
  echo "error: gcc not found. Install build-essential/gcc and required OpenGL/X11 dev packages." >&2
  exit 1
fi

export CGO_ENABLED=1
mkdir -p ./bin

go build -tags gui -o ./bin/duplica-scan-gui ./src/cmd/duplica-scan-gui
echo "Built GUI executable: ./bin/duplica-scan-gui"
