// Package config loads client-side deployment info and defines the list of
// domains kiro-proxy hijacks.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Deployment describes one EC2 hop the client connects to.
//
// Two PSK formats are supported:
//
//  1. Legacy single-user:
//     KIRO_SS_PASS=<16B base64>          -> Password = "<pass>"
//
//  2. Multi-user (SS2022):
//     KIRO_SS_SERVER_KEY=<16B base64>    -> combined as "<server>:<user>"
//     KIRO_SS_USER_KEY=<16B base64>
//     KIRO_SS_USER_NAME=<ascii>          (informational)
type Deployment struct {
	Host     string
	Port     int
	Method   string
	Password string // final string fed to sing-box shadowsocks outbound
	UserName string // empty in legacy mode
	Region   string
}

// LoadDeployment parses a simple KEY=VALUE env file.
func LoadDeployment(path string) (*Deployment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	kv := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		kv[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	d := &Deployment{
		Host:     kv["KIRO_EC2_HOST"],
		Method:   firstNonEmpty(kv["KIRO_SS_METHOD"], "2022-blake3-aes-128-gcm"),
		Region:   kv["REGION"],
		UserName: kv["KIRO_SS_USER_NAME"],
	}
	if d.Host == "" {
		return nil, fmt.Errorf("%s missing KIRO_EC2_HOST", path)
	}

	// Port default 1443, overridable.
	d.Port = 1443
	if p := kv["KIRO_SS_PORT"]; p != "" {
		if _, err := fmt.Sscanf(p, "%d", &d.Port); err != nil {
			return nil, fmt.Errorf("parse KIRO_SS_PORT: %w", err)
		}
	}

	// Resolve PSK: multi-user wins over legacy.
	serverKey := kv["KIRO_SS_SERVER_KEY"]
	userKey := kv["KIRO_SS_USER_KEY"]
	legacyPass := kv["KIRO_SS_PASS"]

	switch {
	case serverKey != "" && userKey != "":
		d.Password = serverKey + ":" + userKey
	case legacyPass != "":
		d.Password = legacyPass
	default:
		return nil, fmt.Errorf("%s needs either KIRO_SS_PASS (legacy) or KIRO_SS_SERVER_KEY + KIRO_SS_USER_KEY (multi-user)", path)
	}

	return d, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
