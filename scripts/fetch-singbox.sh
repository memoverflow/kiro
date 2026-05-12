#!/usr/bin/env bash
# fetch-singbox.sh — download the sing-box darwin-arm64 binary and stage it
# under cmd/kiroctl/embed/ for go:embed inclusion.
#
# We pin the version so the embedded artifact is reproducible. Bump VERSION
# when you want a newer sing-box. Re-run this script after bumping.
#
# Usage:
#   ./scripts/fetch-singbox.sh          # download if missing
#   FORCE=1 ./scripts/fetch-singbox.sh  # re-download even if cached

set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${SINGBOX_VERSION:-1.13.11}"
ARCH_TAG="darwin-arm64"
URL="https://github.com/SagerNet/sing-box/releases/download/v${VERSION}/sing-box-${VERSION}-${ARCH_TAG}.tar.gz"

EMBED_DIR="cmd/kiroctl/cmd/embed"
DST="${EMBED_DIR}/sing-box-darwin-arm64"
VERSION_FILE="${EMBED_DIR}/sing-box.version"

mkdir -p "$EMBED_DIR"

if [[ -f "$DST" && -f "$VERSION_FILE" && "${FORCE:-}" != "1" ]]; then
  have="$(cat "$VERSION_FILE")"
  if [[ "$have" == "$VERSION" ]]; then
    echo "✓ sing-box $VERSION already embedded ($DST)"
    exit 0
  fi
  echo "▸ upgrading embedded sing-box: $have -> $VERSION"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "▸ downloading $URL"
curl -fsSL -o "$tmp/sb.tar.gz" "$URL"

echo "▸ extracting"
tar -xzf "$tmp/sb.tar.gz" -C "$tmp"

# archive layout: sing-box-<ver>-<arch>/sing-box
src="$(find "$tmp" -type f -name sing-box -perm +111 | head -1)"
[[ -n "$src" ]] || { echo "✗ sing-box binary not found in archive" >&2; exit 1; }

install -m 0755 "$src" "$DST"
printf '%s' "$VERSION" > "$VERSION_FILE"

size=$(stat -f %z "$DST" 2>/dev/null || stat -c %s "$DST")
echo "✓ embedded sing-box $VERSION ($((size / 1024 / 1024)) MiB at $DST)"
