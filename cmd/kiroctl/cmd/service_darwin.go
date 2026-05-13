//go:build darwin

package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// InstallService writes the launchd plist and (re)loads it. Idempotent.
func InstallService(kiroctlPath, singBoxPath string) error {
	if err := os.WriteFile(PlistPath, []byte(renderPlist(kiroctlPath, singBoxPath)), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	_ = exec.Command("launchctl", "bootout", "system", PlistPath).Run()
	if out, err := exec.Command("launchctl", "bootstrap", "system", PlistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, out)
	}
	if out, err := exec.Command("launchctl", "kickstart", "-k", "system/"+ServiceLabel).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart: %w: %s", err, out)
	}
	return nil
}

// StopService unloads the launchd job. Best effort.
func StopService() error {
	if _, err := os.Stat(PlistPath); err == nil {
		_ = exec.Command("launchctl", "bootout", "system", PlistPath).Run()
	}
	return nil
}

// ServiceStatus returns a human-readable status blob from launchctl print.
func ServiceStatus() string {
	out, err := exec.Command("launchctl", "print", "system/"+ServiceLabel).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("launchctl: %v\n%s", err, out)
	}
	return string(out)
}

// FlushDNS kicks both resolvers macOS uses in parallel.
func FlushDNS() {
	_ = exec.Command("dscacheutil", "-flushcache").Run()
	_ = exec.Command("killall", "-HUP", "mDNSResponder").Run()
}

func renderPlist(kiroctlPath, singBoxPath string) string {
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
