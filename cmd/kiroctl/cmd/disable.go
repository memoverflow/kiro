package cmd

import (
	"flag"
	"fmt"

	"github.com/xrre/kiro-proxy/pkg/hosts"
)

// Disable restores the hosts file and stops the platform service. Cross-
// platform: launchd on macOS, Windows Service on Windows.
func Disable(args []string) error {
	fs := flag.NewFlagSet("disable", flag.ExitOnError)
	fs.Parse(args)

	mustElevate()

	_ = StopService()

	if err := hosts.Uninstall(); err != nil {
		return fmt.Errorf("restore hosts: %w", err)
	}

	FlushDNS()

	fmt.Println("✓ kiroctl disabled")
	fmt.Println("  hosts restored; sing-box stopped")
	fmt.Println("  Kiro will now connect directly (be careful with IP geofencing)")
	return nil
}
