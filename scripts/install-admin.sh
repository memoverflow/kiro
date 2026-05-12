#!/usr/bin/env bash
# install-admin.sh — build kiro-admin for linux/arm64, scp to EC2, install
# systemd service, initialize admin credentials.
#
# Assumes EC2 already has sing-box running from scripts/deploy-ec2.sh.
# Replaces that single-user config with a multi-user one.
#
# Usage:
#   ./scripts/install-admin.sh
#
# Env overrides:
#   KIRO_ADMIN_USER      admin username (default: admin)
#   KIRO_ADMIN_PASS      admin password (will prompt if empty)

set -euo pipefail
cd "$(dirname "$0")/.."

die() { echo "✗ $*" >&2; exit 1; }
log() { printf "\033[1;36m▸\033[0m %s\n" "$*"; }

[[ -f ./.kiro-proxy.env ]] || die "run scripts/deploy-ec2.sh first"
# shellcheck disable=SC1091
source ./.kiro-proxy.env
: "${KIRO_EC2_HOST:?}" "${SSH_KEY:?}"

log "building kiro-admin for linux/arm64"
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/kiro-admin-linux-arm64 ./cmd/kiro-admin

log "uploading binary to EC2"
scp -i "$SSH_KEY" -o StrictHostKeyChecking=no bin/kiro-admin-linux-arm64 "ubuntu@$KIRO_EC2_HOST:/tmp/kiro-admin"

ADMIN_USER="${KIRO_ADMIN_USER:-admin}"
if [[ -z "${KIRO_ADMIN_PASS:-}" ]]; then
  echo ""
  read -rsp "Pick admin password (min 8 chars) for user '$ADMIN_USER': " ADMIN_PASS
  echo ""
  [[ ${#ADMIN_PASS} -ge 8 ]] || die "password too short"
else
  ADMIN_PASS="$KIRO_ADMIN_PASS"
fi

log "installing on EC2 (systemd + init admin)"
ssh -i "$SSH_KEY" "ubuntu@$KIRO_EC2_HOST" 'sudo bash -s' <<REMOTE
set -e

# Stop current sing-box so we can repoint config path safely.
systemctl stop sing-box 2>/dev/null || true

install -m 0755 /tmp/kiro-admin /usr/local/bin/kiro-admin
rm -f /tmp/kiro-admin

mkdir -p /etc/kiro-admin /etc/sing-box
chmod 700 /etc/kiro-admin

# Init admin password (idempotent).
KIRO_ADMIN_USER="$ADMIN_USER" KIRO_ADMIN_PASS="$ADMIN_PASS" /usr/local/bin/kiro-admin -init-admin -state /etc/kiro-admin

# Render the sing-box config if it doesn't have a users[] yet.
if ! grep -q '"users":' /etc/sing-box/config.json 2>/dev/null; then
  cat > /etc/sing-box/config.json <<'JSON'
{
  "log": { "level": "info", "timestamp": true },
  "inbounds": [
    {
      "type": "shadowsocks",
      "tag": "ss-in",
      "listen": "::",
      "listen_port": 1443,
      "method": "2022-blake3-aes-128-gcm",
      "password": "PLACEHOLDER-WILL-BE-OVERWRITTEN",
      "users": []
    }
  ],
  "outbounds": [ { "type": "direct", "tag": "direct" } ]
}
JSON
fi

# systemd unit for kiro-admin
cat > /etc/systemd/system/kiro-admin.service <<UNIT
[Unit]
Description=kiro-admin web UI
After=network-online.target sing-box.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/kiro-admin -listen 127.0.0.1:8080 -state /etc/kiro-admin -config /etc/sing-box/config.json -eip ${KIRO_EC2_HOST}
Restart=on-failure
RestartSec=2
# Needs root for systemctl reload-or-restart sing-box.
User=root
AmbientCapabilities=
NoNewPrivileges=false

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now kiro-admin
sleep 1
systemctl is-active --quiet kiro-admin || {
  echo "kiro-admin failed to start"
  journalctl -u kiro-admin -n 30 --no-pager
  exit 1
}

# Seed 'admin' Shadowsocks user if no users exist yet, so current client still
# works after migration. The legacy KIRO_SS_PASS from deploy-ec2.sh becomes
# irrelevant after this step — clients must be re-issued env files.
if [[ \$(jq 'length' /etc/kiro-admin/users.json) == "0" ]]; then
  echo "  Creating initial 'admin' user..."
  curl -sS -u '$ADMIN_USER:$ADMIN_PASS' -X POST \
    --data-urlencode 'name=admin' \
    --data-urlencode 'note=auto-created during install' \
    http://127.0.0.1:8080/users/create >/dev/null || true
fi

systemctl start sing-box 2>/dev/null || systemctl restart sing-box
systemctl is-active --quiet sing-box && echo "✓ sing-box active"
REMOTE

cat <<MSG

╭──────────────────────────────────────────────────────────────
│ ✓ kiro-admin installed on EC2
│
│ Access the Web UI via SSH tunnel:
│
│   ssh -i $SSH_KEY -L 8080:127.0.0.1:8080 ubuntu@$KIRO_EC2_HOST
│   open http://127.0.0.1:8080/
│
│ Login: $ADMIN_USER / (your chosen password)
│
│ Next:
│   1. Log in, download 'admin' user's env file
│   2. Save it to ~/.kiro-proxy/config.env
│   3. ./scripts/install-kiroctl.sh
│   4. kiroctl enable
╰──────────────────────────────────────────────────────────────
MSG
