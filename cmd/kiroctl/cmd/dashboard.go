package cmd

import (
	"flag"
	"fmt"
	"os/exec"
)

// Dashboard opens the local Clash UI in the default browser.
func Dashboard(args []string) error {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	fs.Parse(args)
	url := "http://127.0.0.1:9090/ui/"
	if err := exec.Command("open", url).Run(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	fmt.Printf("opened %s\n", url)
	return nil
}
