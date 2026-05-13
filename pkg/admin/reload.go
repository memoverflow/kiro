package admin

import (
	"fmt"
	"os/exec"
)

// ReloadSingBox validates the new sing-box config and reloads the systemd
// service. If validation fails, the service is NOT restarted — the previous
// config stays active.
func ReloadSingBox(configPath string) error {
	if out, err := exec.Command("sing-box", "check", "-c", configPath).CombinedOutput(); err != nil {
		return fmt.Errorf("sing-box check failed: %s", string(out))
	}
	if out, err := exec.Command("systemctl", "reload-or-restart", "sing-box").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl reload-or-restart failed: %s", string(out))
	}
	return nil
}
