#!/usr/bin/env bash
# scripts/build-helper-gui.sh — cross-compile dropsminer-helper-gui
# for Windows / macOS / Linux. The GUI is a tiny localhost-only HTTP
# server that opens the user's default browser to a form, so there's
# no GUI toolkit to link — every target is a plain `go build`.
#
# Usage:
#   scripts/build-helper-gui.sh [version]
#
# Output:
#   dist/dropsminer-helper-gui-<version>-windows-amd64.exe
#   dist/dropsminer-helper-gui-<version>-darwin-arm64
#   dist/dropsminer-helper-gui-<version>-darwin-amd64
#   dist/dropsminer-helper-gui-<version>-linux-amd64
#
# Distribute the raw binaries through GitHub Releases. Non-developer
# friends double-click; the binary opens the helper in their browser.

set -euo pipefail

VERSION="${1:-dev}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$REPO_ROOT/dist"

mkdir -p "$OUT_DIR"
cd "$REPO_ROOT"

LDFLAGS="-s -w -X main.version=$VERSION"

# Build a $name binary for a given $goos/$goarch target.
build() {
  local name="$1" pkg="$2" goos="$3" goarch="$4" suffix="$5"
  local out="$OUT_DIR/$name-$VERSION-$goos-$goarch$suffix"
  echo "→ $name $goos/$goarch  $out"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out" "$pkg"
}

# GUI (localhost browser app) — what Windows users double-click.
build dropsminer-helper-gui ./cmd/dropsminer-helper-gui windows amd64 .exe
build dropsminer-helper-gui ./cmd/dropsminer-helper-gui darwin  arm64 ""
build dropsminer-helper-gui ./cmd/dropsminer-helper-gui darwin  amd64 ""
build dropsminer-helper-gui ./cmd/dropsminer-helper-gui linux   amd64 ""

# CLI — same internal/helper, no GUI. For ops who want a one-liner.
build dropsminer-helper ./cmd/dropsminer-helper windows amd64 .exe
build dropsminer-helper ./cmd/dropsminer-helper darwin  arm64 ""
build dropsminer-helper ./cmd/dropsminer-helper darwin  amd64 ""
build dropsminer-helper ./cmd/dropsminer-helper linux   amd64 ""

echo
echo "Built:"
ls -1 "$OUT_DIR" | sed 's/^/  /'
echo
echo "Upload to GitHub Releases or hand the file directly to the operator."
