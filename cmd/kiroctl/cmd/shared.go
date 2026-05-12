// Package cmd implements kiroctl subcommands.
//
// Shared layout for the Mac side:
//
//	~/.kiro-proxy/config.env     client deployment (from deploy-ec2.sh)
//	~/.kiro-proxy/sing-box.json  generated sing-box config (read by kiroctl only)
//	/Library/LaunchDaemons/io.kiroproxy.sing-box.plist   launchd spec
//	/var/log/kiroproxy.out / .err   sing-box logs
//
// Any file under /Library, /etc, or /var needs sudo. /etc/sudoers.d/kiroctl
// (written by the installer) lets us launchctl (un)load the plist and run
// sing-box without prompting.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	PlistPath     = "/Library/LaunchDaemons/io.kiroproxy.sing-box.plist"
	ServiceLabel  = "io.kiroproxy.sing-box"
	WorkDirSystem = "/Library/Application Support/KiroProxy"
	LogOut        = "/var/log/kiroproxy.out.log"
	LogErr        = "/var/log/kiroproxy.err.log"
)

// SystemConfigPath returns the path sing-box itself reads.
// We keep it outside of the user's home so launchd (running as root) can see it.
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

// SingBoxPath resolves the sing-box binary.
func SingBoxPath() (string, error) {
	if p := os.Getenv("KIRO_SING_BOX"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("sing-box not in PATH (set KIRO_SING_BOX)")
}

// mustSudo re-runs the current process under sudo. It exits on return.
// Used by subcommands that must be root.
func mustSudo() error {
	if os.Geteuid() == 0 {
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
		// If NOPASSWD sudoers is missing, -n fails silently. Fall through
		// to an interactive sudo so the user can type a password once.
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

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 1
}
