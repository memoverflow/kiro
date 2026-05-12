//go:build windows

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// platformPostInstall runs Windows-only bootstrap:
//  1. Ensure %ProgramFiles%\KiroProxy is on the Machine PATH (so a bare
//     `kiroctl` command works in a newly opened shell).
//  2. Add a Windows Defender Firewall inbound rule on :80/:443 so the first
//     `kiroctl enable` doesn't pop an allow-through-firewall prompt in a
//     UAC-spawned console.
func platformPostInstall(opts *InstallOptions, sbPath string) error {
	if err := ensureOnMachinePath(filepath.Dir(opts.BinPath)); err != nil {
		// Non-fatal: user can still call kiroctl by full path.
		fmt.Fprintf(os.Stderr, "⚠ failed to add to PATH: %v\n", err)
	}

	rule := "Kiroctl (SNI proxy)"
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
		fmt.Fprintf(os.Stderr, "⚠ firewall rule add failed (%v): %s\n", err, out)
		return nil
	}
	fmt.Printf("✓ firewall rule added → %s\n", rule)
	return nil
}

// ensureOnMachinePath adds dir to the machine-wide PATH env var, using
// setx /M (which writes to HKLM\Environment). Idempotent: if dir is already
// in PATH we do nothing.
//
// Caveat: new PATH only takes effect in *newly opened* shells. setx also
// truncates values > 1024 chars silently, but we're only appending ~30
// chars so we're fine.
func ensureOnMachinePath(dir string) error {
	// Read current Machine PATH via PowerShell so we hit the persisted value,
	// not this process's inherited (and possibly already-modified) one.
	ps := `[Environment]::GetEnvironmentVariable('Path','Machine')`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	if err != nil {
		return fmt.Errorf("read Machine PATH: %w", err)
	}
	current := strings.TrimSpace(string(out))
	for _, p := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimSpace(p), dir) {
			fmt.Printf("• %s already on PATH\n", dir)
			return nil
		}
	}

	newPath := current
	if newPath != "" && !strings.HasSuffix(newPath, ";") {
		newPath += ";"
	}
	newPath += dir

	// setx truncates at 1024; use the .NET API via PowerShell instead.
	set := fmt.Sprintf(`[Environment]::SetEnvironmentVariable('Path', %q, 'Machine')`, newPath)
	if out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", set).CombinedOutput(); err != nil {
		return fmt.Errorf("set Machine PATH: %w: %s", err, out)
	}
	fmt.Printf("✓ %s added to PATH (open a new PowerShell to pick it up)\n", dir)
	return nil
}
