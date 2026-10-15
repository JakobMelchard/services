#!/bin/sh
# Regenerates docs/lists/screenshots/*.png from internal/lists/data/demo at phone width (390x844).
# Needs bin/lists (make build), node and the repo's node_modules (npm ci); fetches Playwright's Chromium.
set -eu
cd "$(dirname "$0")/../.."
tmp=$(mktemp -d)
cp -R internal/lists/data/demo/. "$tmp"
bin/lists -addr 127.0.0.1:0 -data "$tmp" >"$tmp/log" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null; rm -rf "$tmp"' EXIT
i=0
until grep -q 'lists on' "$tmp/log"; do
  i=$((i + 1)); [ $i -lt 50 ] || { echo "server did not start" >&2; cat "$tmp/log" >&2; exit 1; }
  sleep 0.2
done
addr=$(sed -n 's/.*lists on \([^ ,]*\).*/\1/p' "$tmp/log")
npx --no-install playwright install chromium >/dev/null
node scripts/lists/screenshots.js "http://$addr/" docs/lists/screenshots
