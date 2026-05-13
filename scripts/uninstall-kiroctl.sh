#!/usr/bin/env bash
# uninstall-kiroctl.sh — full rollback: disable, remove sudoers, binary, plist.
set -euo pipefail

die() { echo "✗ $*" >&2; exit 1; }
log() { printf "\033[1;36m▸\033[0m %s\n" "$*"; }

if command -v kiroctl >/dev/null; then
  log "kiroctl disable (best effort)"
  kiroctl disable 2>/dev/null || true
fi

log "removing /etc/sudoers.d/kiroctl"
sudo rm -f /etc/sudoers.d/kiroctl

log "removing /usr/local/bin/kiroctl"
sudo rm -f /usr/local/bin/kiroctl

log "removing /Library/LaunchDaemons/io.kiroproxy.sing-box.plist"
sudo launchctl bootout system/io.kiroproxy.sing-box 2>/dev/null || true
sudo rm -f /Library/LaunchDaemons/io.kiroproxy.sing-box.plist

log "removing /Library/Application Support/KiroProxy"
sudo rm -rf "/Library/Application Support/KiroProxy"

log "removing /var/log/kiroproxy.*"
sudo rm -f /var/log/kiroproxy.*

echo ""
echo "✓ uninstalled. ~/.kiro-proxy/config.env left intact (delete manually if desired)"
