#!/usr/bin/env bash
# Start a local miner on a temp DB for UI work. Usage: dev-miner.sh [--reuse DIR]
set -euo pipefail
PORT="${GRUB_DEV_PORT:-18080}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GO="${GO:-$(command -v go || echo /home/jandro/.local/go/bin/go)}"

if [ "${1:-}" = "--reuse" ]; then
  D="${2:?--reuse needs a directory}"
  [ -x "$D/miner" ] && [ -f "$D/key" ] || { echo "no miner/key in $D" >&2; exit 1; }
else
  D="$(mktemp -d -t grub-dev.XXXXXX)"
  (cd "$ROOT" && "$GO" build -o "$D/miner" ./cmd/miner)
  (umask 077 && "$D/miner" keygen > "$D/key")
  chmod 600 "$D/key"
fi

GRUB_MASTER_KEY="$(cat "$D/key")" GRUB_DB_PATH="$D/miner.db" GRUB_SECURE_COOKIES=0 \
  GRUB_HTTP_ADDR="127.0.0.1:$PORT" GRUB_UPDATE_CHECK=0 \
  nohup "$D/miner" > "$D/miner.log" 2>&1 &
echo $! > "$D/miner.pid"

for _ in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:$PORT/healthz" > /dev/null; then
    echo "GRUB_DEV_DIR=$D"
    exit 0
  fi
  sleep 0.5
done
echo "miner did not become healthy, see $D/miner.log" >&2
exit 1
