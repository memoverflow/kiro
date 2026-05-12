package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/xrre/kiro-proxy/pkg/config"
	"github.com/xrre/kiro-proxy/pkg/hosts"
	"github.com/xrre/kiro-proxy/pkg/singbox"
)

// Enable locks Kiro domains to localhost and starts the local sing-box.
func Enable(args []string) error {
	fs := flag.NewFlagSet("enable", flag.ExitOnError)
	envPath := fs.String("env", EnvFilePath(), "path to client env file")
	fs.Parse(args)

	// Step 1: load deployment (under user creds, no sudo yet).
	dep, err := config.LoadDeployment(*envPath)
	if err != nil {
		return fmt.Errorf("load deployment: %w", err)
	}

	// Step 2: re-exec under sudo for the privileged bits.
	mustSudo()
	// from here on we are root.

	// Step 3: resolve sing-box binary.
	sbPath, err := SingBoxPath()
	if err != nil {
		return err
	}

	// Step 4: make sure our state directory exists.
	if err := os.MkdirAll(WorkDirSystem, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", WorkDirSystem, err)
	}

	// Step 5: generate sing-box config.
	cfgBytes, err := singbox.Generate(singbox.Options{
		Deployment: dep,
		Domains:    config.KiroDomains,
		SocksAddr:  "127.0.0.1:1080",
		CachePath:  WorkDirSystem + "/cache.db",
		ClashAPI:   "127.0.0.1:9090",
		UIDir:      "ui",
	})
	if err != nil {
		return fmt.Errorf("generate sing-box config: %w", err)
	}
	if err := os.WriteFile(SystemConfigPath(), cfgBytes, 0o600); err != nil {
		return fmt.Errorf("write sing-box config: %w", err)
	}

	// Step 6: write launchd plist (idempotent).
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find kiroctl binary: %w", err)
	}
	if err := os.WriteFile(PlistPath, []byte(renderPlist(self, sbPath)), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Step 7: install hosts block.
	if err := hosts.Install(config.KiroDomains); err != nil {
		return fmt.Errorf("install hosts block: %w", err)
	}

	// Step 8: (re)load launchd.
	// bootstrap is idempotent: if already loaded, kickstart just restarts.
	_ = exec.Command("launchctl", "bootout", "system", PlistPath).Run()
	if out, err := exec.Command("launchctl", "bootstrap", "system", PlistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, out)
	}
	if out, err := exec.Command("launchctl", "kickstart", "-k", "system/"+ServiceLabel).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart: %w: %s", err, out)
	}

	// Step 9: flush macOS DNS resolver caches.
	_ = exec.Command("dscacheutil", "-flushcache").Run()
	_ = exec.Command("killall", "-HUP", "mDNSResponder").Run()

	fmt.Printf("✓ kiroctl enabled\n")
	fmt.Printf("  hosts   : %d domains → 127.0.0.1\n", len(config.KiroDomains))
	fmt.Printf("  plist   : %s\n", PlistPath)
	fmt.Printf("  config  : %s\n", SystemConfigPath())
	fmt.Printf("  logs    : %s / %s\n", LogOut, LogErr)
	if dep.UserName != "" {
		fmt.Printf("  user    : %s\n", dep.UserName)
	}
	return nil
}

func renderPlist(kiroctlPath, singBoxPath string) string {
	// launchd runs `kiroctl serve`, which in turn spawns sing-box as a
	// child and itself listens on :443/:80 for SNI hijack. Running the
	// whole thing under one process group makes shutdown clean.
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>-sing-box</string><string>%s</string>
    <string>-config</string><string>%s</string>
    <string>-workdir</string><string>%s</string>
    <string>-sni-addr</string><string>127.0.0.1:443</string>
    <string>-http-addr</string><string>127.0.0.1:80</string>
    <string>-socks-addr</string><string>127.0.0.1:1080</string>
  </array>
  <key>RunAtLoad</key><false/>
  <key>KeepAlive</key><dict><key>Crashed</key><true/></dict>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`, ServiceLabel, kiroctlPath, singBoxPath, SystemConfigPath(), WorkDirSystem, LogOut, LogErr)
}
