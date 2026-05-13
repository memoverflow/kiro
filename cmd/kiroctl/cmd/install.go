package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Install is a one-shot bootstrap for a brand-new machine. Shared plumbing:
//
//  1. Copy the running kiroctl binary to a well-known location on PATH
//  2. Extract the embedded sing-box
//  3. Platform-specific privilege plumbing (sudoers on mac, nothing on win)
//
// Platform specifics live in install_<os>.go — they hand back the target
// binary path for the self-copy, and do any post-copy work (e.g. sudoers).
func Install(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	opts := defaultInstallOptions()
	fs.StringVar(&opts.BinPath, "bin", opts.BinPath, "install location for kiroctl binary")
	platformInstallFlags(fs, opts)
	fs.Parse(args)

	mustElevate()

	// Self-copy.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find self: %w", err)
	}
	self, _ = filepath.EvalSymlinks(self)

	if self != opts.BinPath {
		if err := copyFile(self, opts.BinPath, 0o755); err != nil {
			return fmt.Errorf("install %s -> %s: %w", self, opts.BinPath, err)
		}
		fmt.Printf("✓ kiroctl installed → %s\n", opts.BinPath)
	} else {
		fmt.Printf("• already at %s (skipping self-copy)\n", opts.BinPath)
	}

	// Embedded sing-box.
	sbPath, err := ensureEmbeddedSingBox()
	if err != nil {
		return fmt.Errorf("extract sing-box: %w", err)
	}
	fmt.Printf("✓ sing-box extracted → %s\n", sbPath)

	// Platform-specific post-install (sudoers, firewall rule, …).
	if err := platformPostInstall(opts, sbPath); err != nil {
		return err
	}

	fmt.Print(installNextStepsMessage())
	return nil
}

// InstallOptions is the cross-platform + per-platform config for Install.
// Individual platforms extend it via their own flags in platformInstallFlags.
type InstallOptions struct {
	BinPath string
	// platform extras:
	Sudoers string // darwin only
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

func installNextStepsMessage() string {
	return `
╭──────────────────────────────────────────────────────────────
│ ✓ kiroctl ready
│
│ Next:
│   kiroctl config set-user <name> --server=... --server-key=... --psk=...
│                       (one line; ask your admin for the full command)
│   kiroctl enable
│   kiroctl status
╰──────────────────────────────────────────────────────────────
`
}
