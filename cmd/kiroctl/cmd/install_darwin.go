//go:build darwin

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
)

func defaultInstallOptions() *InstallOptions {
	return &InstallOptions{
		BinPath: "/usr/local/bin/kiroctl",
		Sudoers: "/etc/sudoers.d/kiroctl",
	}
}

func platformInstallFlags(fs *flag.FlagSet, opts *InstallOptions) {
	fs.StringVar(&opts.Sudoers, "sudoers", opts.Sudoers, "sudoers drop-in path")
}

func platformPostInstall(opts *InstallOptions, sbPath string) error {
	// Work out who we're writing sudoers for. SUDO_USER is the un-elevated
	// caller. Fall back to $USER if we somehow got here without going through
	// sudo.
	target := os.Getenv("SUDO_USER")
	if target == "" {
		if u, err := user.Current(); err == nil {
			target = u.Username
		}
	}
	if target == "" || target == "root" {
		return fmt.Errorf("cannot determine non-root user for sudoers (SUDO_USER=%q)", os.Getenv("SUDO_USER"))
	}

	content := fmt.Sprintf(`# Managed by kiroctl install. Do not edit.
%s ALL=(root) NOPASSWD: %s
%s ALL=(root) NOPASSWD: %s
%s ALL=(root) NOPASSWD: /bin/launchctl
%s ALL=(root) NOPASSWD: /usr/bin/dscacheutil
%s ALL=(root) NOPASSWD: /usr/bin/killall -HUP mDNSResponder
`, target, opts.BinPath, target, sbPath, target, target, target)

	tmp, err := os.CreateTemp("", "kiroctl-sudoers-*")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp sudoers: %w", err)
	}
	tmp.Close()

	if out, err := exec.Command("visudo", "-cq", "-f", tmpPath).CombinedOutput(); err != nil {
		return fmt.Errorf("sudoers failed validation: %w: %s", err, out)
	}
	if err := copyFile(tmpPath, opts.Sudoers, 0o440); err != nil {
		return fmt.Errorf("install sudoers: %w", err)
	}
	fmt.Printf("✓ sudoers drop-in → %s (user=%s)\n", opts.Sudoers, target)
	return nil
}
