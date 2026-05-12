package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// Install performs the Mac-side bootstrap for a brand-new machine:
//
//  1. Copy the running kiroctl binary to /usr/local/bin/kiroctl
//  2. Extract the embedded sing-box to /Library/Application Support/KiroProxy/bin/
//  3. Write /etc/sudoers.d/kiroctl so future `kiroctl enable` is NOPASSWD
//
// The idea: ship one file. User runs `./kiroctl install`, enters password
// once, done. No brew, no scripts, no git clone.
func Install(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	binDst := fs.String("bin", "/usr/local/bin/kiroctl", "install location for kiroctl binary")
	sudoers := fs.String("sudoers", "/etc/sudoers.d/kiroctl", "sudoers drop-in path")
	fs.Parse(args)

	// Step 1: re-exec under sudo if needed. Everything below needs root.
	mustSudo()

	// Step 2: resolve where the currently-running kiroctl is, then copy it.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find self: %w", err)
	}
	self, _ = filepath.EvalSymlinks(self)

	if self != *binDst {
		if err := copyFile(self, *binDst, 0o755); err != nil {
			return fmt.Errorf("install %s -> %s: %w", self, *binDst, err)
		}
		fmt.Printf("✓ kiroctl installed → %s\n", *binDst)
	} else {
		fmt.Printf("• already at %s (skipping self-copy)\n", *binDst)
	}

	// Step 3: extract embedded sing-box.
	sbPath, err := ensureEmbeddedSingBox()
	if err != nil {
		return fmt.Errorf("extract sing-box: %w", err)
	}
	fmt.Printf("✓ sing-box extracted → %s\n", sbPath)

	// Step 4: work out who we're writing sudoers for. SUDO_USER is the
	// un-elevated caller. Fall back to $USER if we somehow got here without
	// going through sudo.
	target := os.Getenv("SUDO_USER")
	if target == "" {
		if u, err := user.Current(); err == nil {
			target = u.Username
		}
	}
	if target == "" || target == "root" {
		return fmt.Errorf("cannot determine non-root user for sudoers (SUDO_USER=%q)", os.Getenv("SUDO_USER"))
	}

	// Step 5: write sudoers. Scoped tightly — only the binaries kiroctl
	// actually needs to shell out to.
	content := fmt.Sprintf(`# Managed by kiroctl install. Do not edit.
%s ALL=(root) NOPASSWD: %s
%s ALL=(root) NOPASSWD: %s
%s ALL=(root) NOPASSWD: /bin/launchctl
%s ALL=(root) NOPASSWD: /usr/bin/dscacheutil
%s ALL=(root) NOPASSWD: /usr/bin/killall -HUP mDNSResponder
`, target, *binDst, target, sbPath, target, target, target)

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
	if err := copyFile(tmpPath, *sudoers, 0o440); err != nil {
		return fmt.Errorf("install sudoers: %w", err)
	}
	fmt.Printf("✓ sudoers drop-in → %s (user=%s)\n", *sudoers, target)

	fmt.Printf(`
╭──────────────────────────────────────────────────────────────
│ ✓ kiroctl ready
│
│ Next:
│   kiroctl config set-user <name> --server=... --server-key=... --psk=...
│                       (one line; ask your admin for the full command)
│   sudo kiroctl enable
│   kiroctl status
╰──────────────────────────────────────────────────────────────
`)
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, in, mode); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
