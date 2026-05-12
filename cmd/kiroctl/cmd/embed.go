package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Embedded sing-box binary for darwin-arm64. Fetched by
// scripts/fetch-singbox.sh; if the file is missing at build time, go:embed
// fails the build — that's the point, we'd rather not ship an empty kiroctl.
//
// The bytes live in the text segment; even at ~47 MiB this is fine for a
// one-off distribution binary that an end user runs once.
//
//go:embed embed/sing-box-darwin-arm64
var embeddedSingBox []byte

//go:embed embed/sing-box.version
var embeddedSingBoxVersion string

// embeddedSingBoxFingerprint is the first 12 hex chars of the sha256 of the
// binary. Used to version the cached copy on disk so an upgrade of kiroctl
// re-extracts a fresh sing-box without fighting a running launchd process
// holding the old file open.
func embeddedSingBoxFingerprint() string {
	h := sha256.Sum256(embeddedSingBox)
	return hex.EncodeToString(h[:])[:12]
}

// ensureEmbeddedSingBox materialises the embedded binary to a known location
// and returns its path. Idempotent: rewrites only when the on-disk fingerprint
// doesn't match the embedded one (upgrade case).
//
// Must run as root because the destination lives under /Library.
func ensureEmbeddedSingBox() (string, error) {
	if os.Geteuid() != 0 {
		return "", fmt.Errorf("ensureEmbeddedSingBox: must run as root (got euid=%d)", os.Geteuid())
	}
	dir := filepath.Join(WorkDirSystem, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, "sing-box")
	stamp := filepath.Join(dir, "sing-box.fingerprint")

	want := embeddedSingBoxFingerprint()
	if got, err := os.ReadFile(stamp); err == nil && string(got) == want {
		if _, err := os.Stat(dst); err == nil {
			return dst, nil
		}
	}

	// Atomic-ish write: to a tmp path in the same dir, then rename.
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, embeddedSingBox, 0o755); err != nil {
		return "", fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	if err := os.WriteFile(stamp, []byte(want), 0o644); err != nil {
		return "", fmt.Errorf("write stamp: %w", err)
	}
	return dst, nil
}
