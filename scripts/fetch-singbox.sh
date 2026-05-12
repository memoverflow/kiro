#!/usr/bin/env bash
# fetch-singbox.sh — download sing-box binaries and stage them under
# cmd/kiroctl/cmd/embed/ for go:embed inclusion.
#
# Pinning VERSION keeps embedded artifacts reproducible. Bump VERSION to
# upgrade. Re-run with FORCE=1 to re-download even if cached.
#
# Usage:
#   ./scripts/fetch-singbox.sh                  # fetch all targets (darwin+win)
#   TARGETS="darwin-arm64" ./scripts/fetch-singbox.sh
#   FORCE=1 ./scripts/fetch-singbox.sh

set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${SINGBOX_VERSION:-1.13.11}"
TARGETS="${TARGETS:-darwin-arm64 windows-amd64}"

EMBED_DIR="cmd/kiroctl/cmd/embed"
VERSION_FILE="${EMBED_DIR}/sing-box.version"
mkdir -p "$EMBED_DIR"

fetch_one() {
  local target="$1"
  local archive_ext="tar.gz"
  local dst="${EMBED_DIR}/sing-box-${target}"
  local bin_name="sing-box"
  if [[ "$target" == windows-* ]]; then
    archive_ext="zip"
    dst+=".exe"
    bin_name="sing-box.exe"
  fi

  if [[ -f "$dst" && -f "$VERSION_FILE" && "${FORCE:-}" != "1" ]]; then
    have="$(cat "$VERSION_FILE")"
    if [[ "$have" == "$VERSION" ]]; then
      echo "✓ sing-box $VERSION ($target) already embedded"
      return
    fi
  fi

  local url="https://github.com/SagerNet/sing-box/releases/download/v${VERSION}/sing-box-${VERSION}-${target}.${archive_ext}"
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  echo "▸ downloading $url"
  curl -fsSL -o "$tmp/sb.${archive_ext}" "$url"

  echo "▸ extracting ($target)"
  if [[ "$archive_ext" == "tar.gz" ]]; then
    tar -xzf "$tmp/sb.tar.gz" -C "$tmp"
  else
    unzip -q "$tmp/sb.zip" -d "$tmp"
  fi

  local src
  src="$(find "$tmp" -type f -name "$bin_name" | head -1)"
  [[ -n "$src" ]] || { echo "✗ $bin_name not found in archive for $target" >&2; exit 1; }

  install -m 0755 "$src" "$dst"
  local size
  size=$(stat -f %z "$dst" 2>/dev/null || stat -c %s "$dst")
  echo "✓ embedded sing-box $VERSION $target ($((size / 1024 / 1024)) MiB at $dst)"
}

for t in $TARGETS; do
  fetch_one "$t"
done

printf '%s' "$VERSION" > "$VERSION_FILE"
