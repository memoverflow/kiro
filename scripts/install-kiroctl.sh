#!/usr/bin/env bash
# install-kiroctl.sh — install kiroctl + sudoers rule for passwordless operation.
#
# Idempotent: re-run to upgrade the binary or refresh sudoers.
#
# After install:
#   kiroctl enable
#   kiroctl status
#   kiroctl disable

set -euo pipefail
cd "$(dirname "$0")/.."

die() { echo "✗ $*" >&2; exit 1; }
log() { printf "\033[1;36m▸\033[0m %s\n" "$*"; }

BIN_SRC=bin/kiroctl
BIN_DST=/usr/local/bin/kiroctl
SUDOERS_PATH=/etc/sudoers.d/kiroctl

[[ -x "$BIN_SRC" ]] || { log "building kiroctl (one-off)"; go build -o "$BIN_SRC" ./cmd/kiroctl; }

command -v sing-box >/dev/null || die "sing-box not installed. Run: brew install sing-box"

SING_BOX_PATH="$(command -v sing-box)"
CURRENT_USER="${SUDO_USER:-$(whoami)}"

log "installing kiroctl → $BIN_DST"
sudo install -m 0755 "$BIN_SRC" "$BIN_DST"

log "writing sudoers rule → $SUDOERS_PATH"
# NOPASSWD scoped narrowly:
#   the kiroctl binary itself (it re-execs under sudo)
#   the sing-box binary (so the plist / launchd can keep running root)
#   launchctl bootstrap/bootout/kickstart for our label
#   dscacheutil and killall mDNSResponder for DNS flush
SUDOERS_CONTENT="# Managed by scripts/install-kiroctl.sh. Do not edit.
${CURRENT_USER} ALL=(root) NOPASSWD: ${BIN_DST}
${CURRENT_USER} ALL=(root) NOPASSWD: ${SING_BOX_PATH}
${CURRENT_USER} ALL=(root) NOPASSWD: /bin/launchctl
${CURRENT_USER} ALL=(root) NOPASSWD: /usr/bin/dscacheutil
${CURRENT_USER} ALL=(root) NOPASSWD: /usr/bin/killall -HUP mDNSResponder
"

tmp=$(mktemp)
echo "$SUDOERS_CONTENT" > "$tmp"

# visudo -c to validate before installing
if ! sudo visudo -cq -f "$tmp"; then
  rm -f "$tmp"
  die "sudoers content failed validation — refusing to install"
fi

sudo install -m 0440 "$tmp" "$SUDOERS_PATH"
rm -f "$tmp"

log "verifying NOPASSWD is active"
if ! sudo -n -l "$BIN_DST" >/dev/null 2>&1; then
  die "sudo -n test failed — check $SUDOERS_PATH"
fi

cat <<MSG

╭──────────────────────────────────────────────────────────────
│ ✓ kiroctl installed
│
│ Binary : $BIN_DST
│ Sudo   : $SUDOERS_PATH (NOPASSWD for kiroctl + sing-box)
│
│ Next:
│   kiroctl enable       lock Kiro to EC2
│   kiroctl status       see state
│   kiroctl disable      unlock
│
│ Config : ~/.kiro-proxy/config.env (client env, issued by server-side admin)
│ Monitor: kiroctl dashboard
╰──────────────────────────────────────────────────────────────
MSG
