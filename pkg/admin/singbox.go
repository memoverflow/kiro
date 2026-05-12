package admin

import (
	"encoding/json"
	"fmt"
	"os"
)

// RenderSingBoxConfig writes a sing-box multi-user Shadowsocks 2022 server
// config to path based on the current users + server key.
func RenderSingBoxConfig(store *Store, path string) error {
	users, err := store.List()
	if err != nil {
		return err
	}
	serverKey, err := store.ServerKey()
	if err != nil {
		return err
	}

	usersJSON := make([]map[string]any, 0, len(users))
	for _, u := range users {
		usersJSON = append(usersJSON, map[string]any{
			"name":     u.Name,
			"password": u.Password,
		})
	}

	cfg := map[string]any{
		"log": map[string]any{
			"level":     "info",
			"timestamp": true,
		},
		"inbounds": []any{
			map[string]any{
				"type":        "shadowsocks",
				"tag":         "ss-in",
				"listen":      "::",
				"listen_port": 1443,
				"method":      "2022-blake3-aes-128-gcm",
				"password":    serverKey,
				"users":       usersJSON,
			},
		},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
		},
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
