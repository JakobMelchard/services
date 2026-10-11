#!/bin/sh
# Regenerates docs/lists/screenshots/*.png from internal/lists/data/demo at phone width (390x844).
# Needs bin/lists (make build), node, and a Playwright install with its Chromium:
# NODE_PATH points at a node_modules holding playwright, Chromium comes from
# ~/Library/Caches/ms-playwright.
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
node scripts/lists/screenshots.cjs "http://$addr/" docs/lists/screenshots
