#!/usr/bin/env bash
# build-dist.sh — produce self-contained kiroctl binaries for handoff.
#
# Outputs:
#   dist/kiroctl-darwin-arm64      (macOS Apple Silicon, embeds sing-box)
#   dist/kiroctl-windows-amd64.exe (Windows x64, embeds sing-box.exe)
#
# What the recipient does on a fresh Mac:
#   chmod +x ./kiroctl-darwin-arm64
#   ./kiroctl-darwin-arm64 install
#   kiroctl config set-user <name> --server=... --server-key=... --psk=...
#   sudo kiroctl enable
#
# What the recipient does on a fresh Windows machine (PowerShell):
#   .\kiroctl-windows-amd64.exe install           # UAC prompt x1
#   kiroctl config set-user <name> --server=... --server-key=... --psk=...
#   kiroctl enable                                # UAC prompt x1
#
# No brew, no go, no git required.

set -euo pipefail
cd "$(dirname "$0")/.."

./scripts/fetch-singbox.sh

mkdir -p dist

build_target() {
  local goos="$1" goarch="$2" out="$3"
  echo "▸ building $out ($goos/$goarch)"
  GOOS="$goos" GOARCH="$goarch" go build -ldflags="-s -w" -o "$out" ./cmd/kiroctl
  local size
  size=$(wc -c < "$out" | tr -d '[:space:]')
  echo "✓ $out ($((size / 1024 / 1024)) MiB)"
}

build_target darwin  arm64 dist/kiroctl-darwin-arm64
build_target windows amd64 dist/kiroctl-windows-amd64.exe

cat <<TIP

Handoff:

  macOS (Apple Silicon)
    chmod +x ~/Downloads/kiroctl-darwin-arm64
    xattr -d com.apple.quarantine ~/Downloads/kiroctl-darwin-arm64 2>/dev/null || true
    ./kiroctl-darwin-arm64 install           # enters password once
    kiroctl config set-user <name> --server=... --server-key=... --psk=...
    sudo kiroctl enable

  Windows (x64, PowerShell)
    .\kiroctl-windows-amd64.exe install      # UAC prompt
    # SmartScreen: "More info" → "Run anyway" if blocked
    kiroctl config set-user <name> --server=... --server-key=... --psk=...
    kiroctl enable                           # UAC prompt

TIP
