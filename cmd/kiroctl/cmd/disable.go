package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/xrre/kiro-proxy/pkg/hosts"
)

// Disable restores /etc/hosts and stops the sing-box service.
func Disable(args []string) error {
	fs := flag.NewFlagSet("disable", flag.ExitOnError)
	fs.Parse(args)

	mustSudo()

	// Unload launchd service, ignore errors (may not be loaded).
	if _, err := os.Stat(PlistPath); err == nil {
		_ = exec.Command("launchctl", "bootout", "system", PlistPath).Run()
	}

	if err := hosts.Uninstall(); err != nil {
		return fmt.Errorf("restore hosts: %w", err)
	}

	// DNS cache flush so Kiro immediately sees real IPs again.
	_ = exec.Command("dscacheutil", "-flushcache").Run()
	_ = exec.Command("killall", "-HUP", "mDNSResponder").Run()

	fmt.Println("✓ kiroctl disabled")
	fmt.Println("  /etc/hosts restored; sing-box stopped")
	fmt.Println("  Kiro will now connect directly (be careful with IP geofencing)")
	return nil
}
