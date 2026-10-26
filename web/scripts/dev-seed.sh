#!/usr/bin/env bash
# Seed the dev miner. Needs GRUB_DEV_DIR. Usage: dev-seed.sh [--admin-only]
set -euo pipefail
umask 077
# Targets a fresh DB: setup and games return 409 on a second run.
PORT="${GRUB_DEV_PORT:-18080}"
URL="${GRUB_DEV_URL:-http://127.0.0.1:$PORT}"
D="${GRUB_DEV_DIR:?set GRUB_DEV_DIR to the dev-miner.sh directory}"
JAR="$D/jar"

TOKEN="$(curl -sf -c "$JAR" -b "$JAR" "$URL/api/session" \
  | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>process.stdout.write(JSON.parse(s).csrf_token))')"

post() {
  local path="$1" body="$2" code
  code="$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -c "$JAR" -X POST \
    -H "X-CSRF-Token: $TOKEN" -H 'Content-Type: application/json' -d "$body" "$URL$path")"
  case "$code" in
    2??) ;;
    *) echo "POST $path -> $code" >&2; exit 1 ;;
  esac
}

post /api/setup '{"password":"devpass123","confirm":"devpass123"}'
[ "${1:-}" = "--admin-only" ] && exit 0

for g in "Rust" "Marvel Rivals" "Apex Legends" "Delta Force"; do
  post /api/games "{\"name\":\"$g\"}"
done
post /api/accounts '{"platform":"twitch","name":"seed_twitch"}'
post /api/accounts '{"platform":"kick","name":"seed_kick"}'
