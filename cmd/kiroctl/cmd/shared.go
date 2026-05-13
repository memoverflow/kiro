// Package cmd implements kiroctl subcommands.
//
// Platform-specific state lives in shared_<os>.go under build tags:
//
//	darwin:  /Library/LaunchDaemons/…plist, /Library/Application Support/…
//	windows: %ProgramData%\KiroProxy\… plus a Windows service
//
// The cross-platform code here deals with the user-side config (~/.kiro-proxy/…)
// and the decision tree for locating sing-box.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ServiceLabel identifies our background service to the platform service
// manager. On macOS it's the launchd label; on Windows it's the service name.
const ServiceLabel = "io.kiroproxy.sing-box"

// SystemConfigPath returns the path sing-box itself reads.
// Kept outside the user's home so privileged services can see it.
func SystemConfigPath() string {
	return filepath.Join(WorkDirSystem, "sing-box.json")
}

// UserConfigDir resolves ~/.kiro-proxy.
func UserConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".kiro-proxy"), nil
}

// EnvFilePath returns the deployment env file location.
func EnvFilePath() string {
	if p := os.Getenv("KIRO_PROXY_ENV"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kiro-proxy.env"
	}
	return filepath.Join(home, ".kiro-proxy", "config.env")
}

// SingBoxPath resolves the sing-box binary, in priority order:
//
//  1. KIRO_SING_BOX env var (escape hatch, useful in dev)
//  2. Previously-extracted embedded binary under WorkDirSystem/bin
//  3. If running elevated, extract the embedded copy now and return it
//  4. sing-box in $PATH (e.g. brew install)
//
// Step 3 is what makes a from-scratch install work: the very first privileged
// invocation materialises the bundled binary; everything afterwards re-uses it.
func SingBoxPath() (string, error) {
	if p := os.Getenv("KIRO_SING_BOX"); p != "" {
		return p, nil
	}
	cached := filepath.Join(WorkDirSystem, "bin", singBoxFilename)
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}
	if isElevated() {
		if p, err := ensureEmbeddedSingBox(); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("sing-box not found; run `kiroctl install` first to extract the embedded copy")
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}
