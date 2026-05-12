#!/usr/bin/env bash
# build-dist.sh — produce a single self-contained kiroctl binary for handoff.
#
# Output: dist/kiroctl-darwin-arm64 (~56 MiB, embeds sing-box)
#
# What the recipient does on a fresh Mac:
#   ./kiroctl-darwin-arm64 install
#   ./kiroctl-darwin-arm64 config set-user <name> --server=... --server-key=... --psk=...
#   sudo kiroctl enable
#
# No brew, no go, no git required on their side.

set -euo pipefail
cd "$(dirname "$0")/.."

./scripts/fetch-singbox.sh

mkdir -p dist
OUT="dist/kiroctl-darwin-arm64"

echo "▸ building $OUT"
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o "$OUT" ./cmd/kiroctl

size=$(stat -f %z "$OUT" 2>/dev/null || stat -c %s "$OUT")
echo "✓ $OUT ($((size / 1024 / 1024)) MiB)"

# macOS Gatekeeper quarantine will bite the recipient on first launch. Let
# them know — we can't pre-strip the attribute because it's applied by their
# browser/airdrop/curl on receipt, not by us.
cat <<TIP

Handoff instructions for the recipient:

  1. Drop the file somewhere, e.g. ~/Downloads
  2. xattr -d com.apple.quarantine ~/Downloads/kiroctl-darwin-arm64
  3. ./kiroctl-darwin-arm64 install          # one-time, needs sudo password
  4. kiroctl config set-user <name> --server=... --server-key=... --psk=...
  5. sudo kiroctl enable

TIP
