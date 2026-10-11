#!/bin/sh
# Regenerate docs/workouts/screenshots/*.png from the demo data in cmd/demo (build tag demo,
# fake in-memory Sheets, never talks to Google). Needs Go (cgo) and Node (npx).
# PORT defaults to 8561.
set -eu
cd "$(dirname "$0")/../.."
port=${PORT:-8561}
tmp=$(mktemp -d)
go build -tags demo -o "$tmp/demo" ./cmd/demo
"$tmp/demo" -addr "127.0.0.1:$port" &
pid=$!
trap 'kill $pid 2>/dev/null; rm -rf "$tmp"' EXIT
i=0
until curl -fs "http://127.0.0.1:$port/" >/dev/null; do
  i=$((i + 1)); [ $i -lt 50 ] || { echo "server did not start" >&2; exit 1; }
  sleep 0.2
done
npx -y -p playwright@1.63.0 sh -c 'playwright install chromium >/dev/null &&
  NODE_PATH="$(dirname "$(dirname "$(command -v playwright)")")" node scripts/workouts/screenshots.js "$@"' \
  sh "http://127.0.0.1:$port/" docs/workouts/screenshots
