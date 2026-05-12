//go:build darwin

package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// macOS-specific paths.
const (
	PlistPath       = "/Library/LaunchDaemons/io.kiroproxy.sing-box.plist"
	WorkDirSystem   = "/Library/Application Support/KiroProxy"
	LogOut          = "/var/log/kiroproxy.out.log"
	LogErr          = "/var/log/kiroproxy.err.log"
	singBoxFilename = "sing-box"
)

// isElevated reports whether we're running with the privileges needed to
// touch /Library, /etc, /var, and load launchd daemons.
func isElevated() bool { return os.Geteuid() == 0 }

// mustElevate re-execs the current process under sudo, prompting the user
// for a password only if the NOPASSWD sudoers rule isn't installed yet.
// Exits when the child returns.
func mustElevate() error {
	if isElevated() {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find self: %w", err)
	}
	args := append([]string{"-n", self}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// -n failed (no NOPASSWD) → fall back to interactive sudo.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			fmt.Fprintln(os.Stderr, "(sudo NOPASSWD rule not found; falling back to interactive sudo)")
			interactive := exec.Command("sudo", append([]string{self}, os.Args[1:]...)...)
			interactive.Stdin = os.Stdin
			interactive.Stdout = os.Stdout
			interactive.Stderr = os.Stderr
			err = interactive.Run()
		}
		if err != nil {
			os.Exit(exitCode(err))
		}
	}
	os.Exit(0)
	return nil
}
