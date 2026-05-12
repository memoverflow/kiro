//go:build windows

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Default install location: %ProgramFiles%\KiroProxy\kiroctl.exe.
// Windows 10+ users have %ProgramFiles% on PATH is NOT automatic, but
// services.msc will call it by absolute path so PATH membership is a
// nice-to-have, not a requirement.
func defaultInstallOptions() *InstallOptions {
	base := os.Getenv("ProgramFiles")
	if base == "" {
		base = `C:\Program Files`
	}
	return &InstallOptions{
		BinPath: filepath.Join(base, "KiroProxy", "kiroctl.exe"),
	}
}

func platformInstallFlags(fs *flag.FlagSet, opts *InstallOptions) {
	// No Windows-specific flags yet. PATH munging could go here later.
}

// platformPostInstall adds a Windows Defender Firewall inbound rule so the
// first `kiroctl enable` doesn't pop a "Allow Kiroctl to communicate through
// the firewall" prompt in a UAC-spawned console the user can't easily click.
//
// We scope the rule to localhost:443/:80 which is where SNI hijack listens.
func platformPostInstall(opts *InstallOptions, sbPath string) error {
	rule := "Kiroctl (SNI proxy)"

	// Try to delete any old version so re-runs don't stack duplicate rules.
	_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+rule).Run()

	out, err := exec.Command(
		"netsh", "advfirewall", "firewall", "add", "rule",
		"name="+rule,
		"dir=in",
		"action=allow",
		"protocol=TCP",
		"localport=80,443",
		"profile=any",
		"program="+opts.BinPath,
	).CombinedOutput()
	if err != nil {
		// Non-fatal — the user can still run, Windows will just prompt them
		// once on first launch.
		fmt.Fprintf(os.Stderr, "⚠ firewall rule add failed (%v): %s\n", err, out)
		return nil
	}
	fmt.Printf("✓ firewall rule added → %s\n", rule)
	return nil
}
