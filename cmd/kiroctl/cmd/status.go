package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xrre/kiro-proxy/pkg/hosts"
)

// Status prints whether kiroctl is currently engaged.
func Status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	verbose := fs.Bool("v", false, "verbose: dump every intercepted domain")
	fs.Parse(args)

	// 1. /etc/hosts block state.
	installed, err := hosts.IsInstalled()
	if err != nil {
		return fmt.Errorf("check hosts: %w", err)
	}

	var hostedDomains []string
	if installed {
		hostedDomains, _ = hosts.ListInstalled()
	}

	// 2. Platform service state (launchd / Windows Service).
	sbState := serviceStatusLine()

	// 3. Clash API liveness.
	clashAlive, clashSummary := clashStatus()

	// 4. Pretty print.
	statusLine := func(name, v string) {
		fmt.Printf("  %-14s %s\n", name+":", v)
	}

	fmt.Println("kiroctl status")
	statusLine("hosts", boolTag(installed, fmt.Sprintf("locked (%d domains)", len(hostedDomains)), "not locked"))
	statusLine("sing-box", sbState)
	statusLine("tunnel", boolTag(clashAlive, clashSummary, "no clash-api (sing-box not running?)"))

	if installed && sbState != "" && clashAlive {
		fmt.Println()
		fmt.Println("  Kiro traffic is forced through the EC2 tunnel.")
	} else {
		fmt.Println()
		fmt.Println("  ⚠ Kiro may be able to connect directly. Run `kiroctl enable` to lock it.")
	}

	if *verbose && installed {
		fmt.Println()
		fmt.Println("  Intercepted domains:")
		for _, d := range hostedDomains {
			fmt.Printf("    %s\n", d)
		}
	}

	return nil
}

func boolTag(ok bool, trueMsg, falseMsg string) string {
	if ok {
		return "✓ " + trueMsg
	}
	return "✗ " + falseMsg
}

// serviceStatusLine condenses the platform-specific ServiceStatus() blob
// into a single user-facing line.
func serviceStatusLine() string {
	text := ServiceStatus()
	switch {
	case text == "":
		return "✗ not loaded"
	case strings.Contains(text, "state = running"), strings.Contains(text, "state=Running"):
		return "✓ running"
	case strings.Contains(text, "state = waiting"):
		return "⚠ waiting (will restart on demand)"
	case strings.Contains(text, "state=Stopped"), strings.Contains(text, "not installed"):
		return "✗ not running"
	}
	return "⚠ loaded (unknown state)"
}

func clashStatus() (bool, string) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:9090/connections")
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var d struct {
		UploadTotal   uint64 `json:"uploadTotal"`
		DownloadTotal uint64 `json:"downloadTotal"`
		Connections   []any  `json:"connections"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return false, ""
	}
	return true, fmt.Sprintf("alive (↑ %s  ↓ %s  active=%d)", humanBytes(d.UploadTotal), humanBytes(d.DownloadTotal), len(d.Connections))
}

func humanBytes(n uint64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/1024/1024/1024)
	}
}

