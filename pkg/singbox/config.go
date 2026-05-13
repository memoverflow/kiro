// Package singbox generates the sing-box config kiroctl spawns.
//
// Role: sing-box is the SOCKS5 outbound that carries sniffed hostnames from
// the kiroctl SNI proxy to the EC2 Shadowsocks endpoint. It does NOT listen
// on :443/:80 directly — that job belongs to pkg/sni because sing-box's
// `direct` inbound can't rewrite destination from sniffed SNI.
//
// Two listeners:
//   - 127.0.0.1:1080    SOCKS5 inbound (plain TCP) for the SNI proxy
//   - 127.0.0.1:9090    Clash API for monitoring (optional)
//
// The shadowsocks outbound carries (host, port) tuples to EC2, where the
// real DNS resolution happens.
package singbox

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"

	"github.com/xrre/kiro-proxy/pkg/config"
)

// Options configures the generated config.
type Options struct {
	Deployment *config.Deployment
	Domains    []string // domains allowed to reach the EC2 outbound; others → reject
	SocksAddr  string   // "127.0.0.1:1080" — SNI proxy will connect here
	CachePath  string   // /tmp/kiro-proxy.cache etc.
	ClashAPI   string   // "127.0.0.1:9090" for monitoring; empty disables
	UIDir      string   // relative to -D working dir
}

// Generate returns the sing-box config JSON.
func Generate(opt Options) ([]byte, error) {
	if opt.Deployment == nil {
		return nil, fmt.Errorf("deployment is required")
	}
	if opt.SocksAddr == "" {
		opt.SocksAddr = "127.0.0.1:1080"
	}
	d := opt.Deployment

	host, port, err := splitHostPort(opt.SocksAddr)
	if err != nil {
		return nil, err
	}

	cfg := map[string]any{
		"log": map[string]any{
			"level":     "info",
			"timestamp": true,
		},

		"inbounds": []any{
			map[string]any{
				"type":        "socks",
				"tag":         "socks-in",
				"listen":      host,
				"listen_port": port,
			},
		},

		"outbounds": []any{
			map[string]any{
				"type":        "shadowsocks",
				"tag":         "ec2",
				"server":      d.Host,
				"server_port": d.Port,
				"method":      d.Method,
				"password":    d.Password,
			},
			map[string]any{"type": "direct", "tag": "direct"},
		},

		"route": map[string]any{
			"rules": []any{
				// Allowlist → EC2.
				map[string]any{
					"rule_set": []string{"kiro-domains"},
					"outbound": "ec2",
				},
				// Anything that reached this SOCKS inbound without matching
				// is suspicious — fail-closed.
				map[string]any{"action": "reject"},
			},
			"rule_set": []any{
				map[string]any{
					"type":   "inline",
					"tag":    "kiro-domains",
					"format": "source",
					"rules": []any{
						map[string]any{"domain": opt.Domains},
					},
				},
			},
			"final": "direct",
		},
	}

	experimental := map[string]any{}
	if opt.CachePath != "" {
		experimental["cache_file"] = map[string]any{
			"enabled": true,
			"path":    opt.CachePath,
		}
	}
	if opt.ClashAPI != "" {
		clash := map[string]any{
			"external_controller": opt.ClashAPI,
			"secret":              "",
			"default_mode":        "rule",
		}
		if opt.UIDir != "" {
			clash["external_ui"] = opt.UIDir
			clash["external_ui_download_url"] = "https://github.com/MetaCubeX/metacubexd/archive/refs/heads/gh-pages.zip"
			clash["external_ui_download_detour"] = "ec2"
		}
		experimental["clash_api"] = clash
	}
	if len(experimental) > 0 {
		cfg["experimental"] = experimental
	}

	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return buf, nil
}

func splitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("parse %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("parse port in %q: %w", addr, err)
	}
	return host, port, nil
}
