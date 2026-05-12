package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/xrre/kiro-proxy/pkg/config"
)

// Config dispatches `kiroctl config <verb>` subcommands.
//
// Verbs:
//
//	set-user NAME --server= --server-key= --psk= [--method=] [--user=]
//	get-contexts
//	current-context
//	use-context NAME
//	delete-context NAME
//	view
//
// All verbs operate on ~/.kiro-proxy/config.json (kubectl-style).
func Config(args []string) error {
	if len(args) == 0 {
		return configUsage()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "set-user":
		return configSetUser(rest)
	case "get-contexts":
		return configGetContexts()
	case "current-context":
		return configCurrentContext()
	case "use-context":
		return configUseContext(rest)
	case "delete-context":
		return configDeleteContext(rest)
	case "view":
		return configView()
	case "help", "-h", "--help":
		return configUsage()
	default:
		return fmt.Errorf("unknown config verb: %q (try 'kiroctl config help')", verb)
	}
}

func configSetUser(args []string) error {
	// Pull NAME out before flag parsing. Go's flag package stops at the first
	// non-flag arg, so we'd otherwise force users to put NAME *after* all
	// flags — surprising and unlike kubectl.
	name, flagArgs, err := extractPositional(args)
	if err != nil {
		return fmt.Errorf("usage: kiroctl config set-user NAME --server=... --server-key=... --psk=...")
	}

	fs := flag.NewFlagSet("config set-user", flag.ExitOnError)
	server := fs.String("server", "", "server host:port, e.g. 54.x.x.x:1443")
	serverKey := fs.String("server-key", "", "shared server PSK (base64)")
	psk := fs.String("psk", "", "per-user PSK (base64)")
	method := fs.String("method", config.DefaultMethod, "shadowsocks method")
	user := fs.String("user", "", "user name inside shadowsocks (defaults to NAME)")
	fs.Parse(flagArgs)

	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected extra args: %v", fs.Args())
	}
	if *server == "" || *serverKey == "" || *psk == "" {
		return fmt.Errorf("--server, --server-key, --psk are all required")
	}

	path, err := config.KubeconfigPath()
	if err != nil {
		return err
	}
	kc, err := config.LoadKubeconfig(path)
	if err != nil {
		return err
	}
	userName := *user
	if userName == "" {
		userName = name
	}
	kc.SetUser(name, config.Context{
		Server:    *server,
		Method:    *method,
		ServerKey: *serverKey,
		User:      userName,
		PSK:       *psk,
	})
	if err := kc.Save(path); err != nil {
		return err
	}
	fmt.Printf("✓ context %q written to %s\n", name, path)
	fmt.Printf("  current-context: %s\n", kc.CurrentContext)
	fmt.Printf("  next: sudo kiroctl enable\n")
	return nil
}

func configGetContexts() error {
	kc, _, err := loadKC()
	if err != nil {
		return err
	}
	names := kc.ContextNames()
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "(no contexts)")
		return nil
	}
	for _, n := range names {
		marker := " "
		if n == kc.CurrentContext {
			marker = "*"
		}
		ctx := kc.Contexts[n]
		fmt.Printf("%s %-20s %s (user=%s)\n", marker, n, ctx.Server, ctx.User)
	}
	return nil
}

func configCurrentContext() error {
	kc, _, err := loadKC()
	if err != nil {
		return err
	}
	if kc.CurrentContext == "" {
		return fmt.Errorf("no current context set")
	}
	fmt.Println(kc.CurrentContext)
	return nil
}

func configUseContext(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kiroctl config use-context NAME")
	}
	kc, path, err := loadKC()
	if err != nil {
		return err
	}
	if err := kc.UseContext(args[0]); err != nil {
		return err
	}
	if err := kc.Save(path); err != nil {
		return err
	}
	fmt.Printf("✓ switched to %q\n", args[0])
	return nil
}

func configDeleteContext(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kiroctl config delete-context NAME")
	}
	kc, path, err := loadKC()
	if err != nil {
		return err
	}
	if err := kc.DeleteContext(args[0]); err != nil {
		return err
	}
	if err := kc.Save(path); err != nil {
		return err
	}
	fmt.Printf("✓ deleted %q\n", args[0])
	return nil
}

func configView() error {
	_, path, err := loadKC()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// Redact PSK / server-key: print but truncate each to first 4 chars + "…".
	text := string(b)
	text = redactKeys(text)
	fmt.Print(text)
	if !strings.HasSuffix(text, "\n") {
		fmt.Println()
	}
	fmt.Fprintln(os.Stderr, "(psk / server-key redacted — file lives at "+path+")")
	return nil
}

// extractPositional pulls the first non-flag argument out of args and returns
// the rest (in original order). Returns error if no positional is found.
//
// We accept both "NAME --flag=x" and "--flag=x NAME" forms — matches how
// kubectl handles `kubectl config set-cluster NAME --server=...`.
func extractPositional(args []string) (string, []string, error) {
	rest := make([]string, 0, len(args))
	name := ""
	for _, a := range args {
		if name == "" && !strings.HasPrefix(a, "-") {
			name = a
			continue
		}
		rest = append(rest, a)
	}
	if name == "" {
		return "", nil, fmt.Errorf("missing NAME")
	}
	return name, rest, nil
}

func loadKC() (*config.Kubeconfig, string, error) {
	path, err := config.KubeconfigPath()
	if err != nil {
		return nil, "", err
	}
	kc, err := config.LoadKubeconfig(path)
	if err != nil {
		return nil, "", err
	}
	return kc, path, nil
}

// redactKeys masks the high-entropy fields so `config view` can be pasted
// into a bug report without leaking credentials.
func redactKeys(s string) string {
	for _, field := range []string{`"psk"`, `"server-key"`} {
		idx := 0
		for {
			k := strings.Index(s[idx:], field)
			if k == -1 {
				break
			}
			k += idx
			// find opening quote of value
			start := strings.Index(s[k:], `"`)
			if start == -1 {
				break
			}
			start = k + start + len(field) // skip field
			// hop past : and whitespace
			vStart := strings.Index(s[start:], `"`)
			if vStart == -1 {
				break
			}
			vStart = start + vStart + 1
			vEnd := strings.Index(s[vStart:], `"`)
			if vEnd == -1 {
				break
			}
			vEnd = vStart + vEnd
			val := s[vStart:vEnd]
			keep := 4
			if len(val) < keep {
				keep = len(val)
			}
			redacted := val[:keep] + "…(redacted)"
			s = s[:vStart] + redacted + s[vEnd:]
			idx = vStart + len(redacted)
		}
	}
	return s
}

func configUsage() error {
	fmt.Fprintln(os.Stderr, `kiroctl config — manage client contexts (kubectl-style).

Usage:
  kiroctl config set-user NAME --server=HOST:PORT --server-key=PSK --psk=PSK [--method=M] [--user=U]
  kiroctl config get-contexts
  kiroctl config current-context
  kiroctl config use-context NAME
  kiroctl config delete-context NAME
  kiroctl config view

File:
  ~/.kiro-proxy/config.json  (override with KIRO_CONFIG)`)
	return nil
}
