// kubeconfig.go — kubectl-style client config.
//
// File layout at ~/.kiro-proxy/config.json:
//
//	{
//	  "current-context": "alice",
//	  "contexts": {
//	    "alice": {
//	      "server":     "54.x.x.x:1443",
//	      "method":     "2022-blake3-aes-128-gcm",
//	      "server-key": "…base64…",
//	      "user":       "alice",
//	      "psk":        "…base64…"
//	    }
//	  }
//	}
//
// One context per Shadowsocks identity. `kiroctl config set-user NAME …`
// creates or overwrites the NAME context and flips current-context to it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const DefaultMethod = "2022-blake3-aes-128-gcm"

// ErrNoContext surfaces when current-context points at nothing (or the file
// is missing). Callers use it to decide whether to fall back to env.
var ErrNoContext = errors.New("no current context")

// Context is one connection identity: the server, the credentials, the user
// name. Fields are JSON-tagged to match the on-disk kebab-case keys.
type Context struct {
	Server    string `json:"server"`
	Method    string `json:"method,omitempty"`
	ServerKey string `json:"server-key"`
	User      string `json:"user"`
	PSK       string `json:"psk"`
}

// Kubeconfig is the top-level container.
type Kubeconfig struct {
	CurrentContext string             `json:"current-context"`
	Contexts       map[string]Context `json:"contexts"`
}

// KubeconfigPath resolves the default client config location.
func KubeconfigPath() (string, error) {
	if p := os.Getenv("KIRO_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".kiro-proxy", "config.json"), nil
}

// LoadKubeconfig reads the file. A missing file returns an empty (but non-nil)
// config so callers can add to it.
func LoadKubeconfig(path string) (*Kubeconfig, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Kubeconfig{Contexts: map[string]Context{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := &Kubeconfig{}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]Context{}
	}
	return cfg, nil
}

// Save writes atomically with 0600 perms.
func (k *Kubeconfig) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}

// Current returns the active context. Returns ErrNoContext when the file has
// no contexts yet or current-context points at a missing entry.
func (k *Kubeconfig) Current() (string, Context, error) {
	if k.CurrentContext == "" {
		return "", Context{}, ErrNoContext
	}
	c, ok := k.Contexts[k.CurrentContext]
	if !ok {
		return "", Context{}, fmt.Errorf("current-context %q not defined: %w", k.CurrentContext, ErrNoContext)
	}
	return k.CurrentContext, c, nil
}

// SetUser creates or overwrites a named context and marks it current.
// Called by `kiroctl config set-user NAME …` — a one-shot onboarding command.
func (k *Kubeconfig) SetUser(name string, ctx Context) {
	if k.Contexts == nil {
		k.Contexts = map[string]Context{}
	}
	if ctx.Method == "" {
		ctx.Method = DefaultMethod
	}
	if ctx.User == "" {
		ctx.User = name
	}
	k.Contexts[name] = ctx
	k.CurrentContext = name
}

// UseContext switches the active context. Errors if unknown.
func (k *Kubeconfig) UseContext(name string) error {
	if _, ok := k.Contexts[name]; !ok {
		return fmt.Errorf("context %q not found", name)
	}
	k.CurrentContext = name
	return nil
}

// DeleteContext removes the named context. Clears current-context if it
// pointed at the deleted one.
func (k *Kubeconfig) DeleteContext(name string) error {
	if _, ok := k.Contexts[name]; !ok {
		return fmt.Errorf("context %q not found", name)
	}
	delete(k.Contexts, name)
	if k.CurrentContext == name {
		k.CurrentContext = ""
	}
	return nil
}

// ContextNames returns a sorted list of context names.
func (k *Kubeconfig) ContextNames() []string {
	names := make([]string, 0, len(k.Contexts))
	for n := range k.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// toDeployment projects a Context into the runtime-facing Deployment struct.
func (c Context) toDeployment() (*Deployment, error) {
	host, port, err := splitHostPort(c.Server)
	if err != nil {
		return nil, err
	}
	if c.ServerKey == "" || c.PSK == "" {
		return nil, fmt.Errorf("context missing server-key or psk")
	}
	method := c.Method
	if method == "" {
		method = DefaultMethod
	}
	return &Deployment{
		Host:     host,
		Port:     port,
		Method:   method,
		Password: c.ServerKey + ":" + c.PSK,
		UserName: c.User,
	}, nil
}

// splitHostPort parses "host:port" with sane defaults (port=1443).
func splitHostPort(s string) (string, int, error) {
	if s == "" {
		return "", 0, fmt.Errorf("empty server")
	}
	// Look for the last colon — IPv4 / DNS names only, so this is safe.
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			var port int
			if _, err := fmt.Sscanf(s[i+1:], "%d", &port); err != nil {
				return "", 0, fmt.Errorf("parse port in %q: %w", s, err)
			}
			return s[:i], port, nil
		}
	}
	return s, 1443, nil
}
