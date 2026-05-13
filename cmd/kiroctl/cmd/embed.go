package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// The embedded sing-box bytes and version string come from platform-specific
// files (embed_darwin.go, embed_windows.go) — each wires its own go:embed
// directive against the right binary. This file is just the deploy logic.

// embeddedSingBoxFingerprint is the first 12 hex chars of the sha256 of the
// binary. Used to version the cached copy on disk so an upgrade of kiroctl
// re-extracts a fresh sing-box without fighting a running service holding
// the old file open.
func embeddedSingBoxFingerprint() string {
	h := sha256.Sum256(embeddedSingBox)
	return hex.EncodeToString(h[:])[:12]
}

// ensureEmbeddedSingBox materialises the embedded binary to a known location
// and returns its path. Idempotent: only rewrites when the on-disk fingerprint
// doesn't match the embedded one (upgrade case).
//
// Must be called with elevated privileges because the destination lives under
// a system directory.
func ensureEmbeddedSingBox() (string, error) {
	if !isElevated() {
		return "", fmt.Errorf("ensureEmbeddedSingBox: must run elevated (try `kiroctl install` or `kiroctl enable`)")
	}
	dir := filepath.Join(WorkDirSystem, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, singBoxFilename)
	stamp := filepath.Join(dir, "sing-box.fingerprint")

	want := embeddedSingBoxFingerprint()
	if got, err := os.ReadFile(stamp); err == nil && string(got) == want {
		if _, err := os.Stat(dst); err == nil {
			return dst, nil
		}
	}

	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, embeddedSingBox, 0o755); err != nil {
		return "", fmt.Errorf("write %s: %w", tmp, err)
	}
	// On Windows we can't rename over a running exe; but the service was
	// stopped before this is called (enable flow restarts it), so this is
	// fine. Worst case the caller sees "access denied" and retries.
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	if err := os.WriteFile(stamp, []byte(want), 0o644); err != nil {
		return "", fmt.Errorf("write stamp: %w", err)
	}
	return dst, nil
}
