#!/usr/bin/env bash
# scripts/release.sh — tag and push miner + browser images to ghcr.io.
#
# Usage:
#   scripts/release.sh v0.1.0             # tag images and push
#   scripts/release.sh v0.1.0 --build-only # build but don't push
#
# Prereqs:
#   docker login ghcr.io -u <github-user>  (PAT with packages:write)

set -euo pipefail

TAG="${1:-}"
MODE="${2:-push}"

if [[ -z "$TAG" ]]; then
  echo "usage: $0 <tag> [--build-only]" >&2
  exit 2
fi

REGISTRY="ghcr.io/chano-fernandez"
MINER_IMAGE="$REGISTRY/rust-drops-miner"
BROWSER_IMAGE="$REGISTRY/rust-drops-miner-browser"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== Building $MINER_IMAGE:$TAG ==="
docker build -f deploy/Dockerfile.miner -t "$MINER_IMAGE:$TAG" -t "$MINER_IMAGE:latest" .

echo "=== Building $BROWSER_IMAGE:$TAG ==="
docker build -f deploy/Dockerfile.browser -t "$BROWSER_IMAGE:$TAG" -t "$BROWSER_IMAGE:latest" .

if [[ "$MODE" == "--build-only" ]]; then
  echo "Build-only mode; not pushing."
  exit 0
fi

echo "=== Pushing miner ==="
docker push "$MINER_IMAGE:$TAG"
docker push "$MINER_IMAGE:latest"

echo "=== Pushing browser ==="
docker push "$BROWSER_IMAGE:$TAG"
docker push "$BROWSER_IMAGE:latest"

echo
echo "Released:"
echo "  $MINER_IMAGE:$TAG"
echo "  $BROWSER_IMAGE:$TAG"
echo
echo "Next: update humblewhale/rust-drops-miner/compose.yml image tags, commit, deploy."
