package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xrre/kiro-proxy/pkg/config"
	"github.com/xrre/kiro-proxy/pkg/hosts"
	"github.com/xrre/kiro-proxy/pkg/singbox"
)

// Enable locks Kiro domains to localhost and starts the platform service.
// Cross-platform: launchd on macOS, Windows Service on Windows.
func Enable(args []string) error {
	fs := flag.NewFlagSet("enable", flag.ExitOnError)
	envPath := fs.String("env", EnvFilePath(), "path to legacy env file (fallback)")
	fs.Parse(args)

	// Step 1: load deployment before elevation so config errors surface as
	// the current user, not in a UAC-spawned window the user can't see.
	dep, err := config.LoadDeployment(*envPath)
	if err != nil {
		return fmt.Errorf("load deployment: %w", err)
	}

	// Step 2: elevate (sudo on mac, UAC on windows).
	mustElevate()

	// Step 3: resolve sing-box binary (extracts embedded copy if first run).
	sbPath, err := SingBoxPath()
	if err != nil {
		return err
	}

	// Step 4: ensure state directory + log directory exist.
	if err := os.MkdirAll(WorkDirSystem, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", WorkDirSystem, err)
	}
	if err := os.MkdirAll(filepath.Dir(LogOut), 0o755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}

	// Step 5: render sing-box config.
	cfgBytes, err := singbox.Generate(singbox.Options{
		Deployment: dep,
		Domains:    config.KiroDomains,
		SocksAddr:  "127.0.0.1:1080",
		CachePath:  filepath.Join(WorkDirSystem, "cache.db"),
		ClashAPI:   "127.0.0.1:9090",
		UIDir:      "ui",
	})
	if err != nil {
		return fmt.Errorf("generate sing-box config: %w", err)
	}
	if err := os.WriteFile(SystemConfigPath(), cfgBytes, 0o600); err != nil {
		return fmt.Errorf("write sing-box config: %w", err)
	}

	// Step 6: install hosts block. Done before starting the service so the
	// first packet already hits our SNI proxy.
	if err := hosts.Install(config.KiroDomains); err != nil {
		return fmt.Errorf("install hosts block: %w", err)
	}

	// Step 7: platform-specific service registration + start.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find kiroctl binary: %w", err)
	}
	if err := InstallService(self, sbPath); err != nil {
		return fmt.Errorf("install service: %w", err)
	}

	// Step 8: flush DNS so cached answers don't resolve to real IPs.
	FlushDNS()

	fmt.Printf("✓ kiroctl enabled\n")
	fmt.Printf("  hosts   : %d domains → 127.0.0.1\n", len(config.KiroDomains))
	fmt.Printf("  service : %s\n", ServiceLabel)
	fmt.Printf("  config  : %s\n", SystemConfigPath())
	fmt.Printf("  logs    : %s\n", LogOut)
	if dep.UserName != "" {
		fmt.Printf("  user    : %s\n", dep.UserName)
	}
	return nil
}
