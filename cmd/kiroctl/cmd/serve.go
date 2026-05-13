package cmd

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/xrre/kiro-proxy/pkg/sni"
)

// Serve is the daemon body that launchd / Windows Service runs. On macOS it
// simply runs serveCore in the foreground. On Windows it detects whether SCM
// started us — if so it hands control to svc.Run() so we can report state
// transitions back to the SCM and avoid error 1053 ("service did not respond
// to start request in a timely fashion").
//
// Not meant to be called by humans directly. `kiroctl enable` registers the
// platform service which then invokes `kiroctl serve`.
func Serve(args []string) error {
	opts, err := parseServeArgs(args)
	if err != nil {
		return err
	}
	return platformServe(opts)
}

// serveOptions bundles the flag values so we can pass them around easily
// between platform entry points and serveCore.
type serveOptions struct {
	SingBox   string
	CfgPath   string
	WorkDir   string
	SNIAddr   string
	HTTPAddr  string
	SocksAddr string
}

func parseServeArgs(args []string) (*serveOptions, error) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	singBox := fs.String("sing-box", defaultSingBoxDev, "sing-box binary path")
	cfgPath := fs.String("config", SystemConfigPath(), "sing-box config path")
	workDir := fs.String("workdir", WorkDirSystem, "sing-box working directory")
	sniAddr := fs.String("sni-addr", "127.0.0.1:443", "SNI proxy listen address")
	httpAddr := fs.String("http-addr", "127.0.0.1:80", "plain HTTP hijack listen address (empty to disable)")
	socksAddr := fs.String("socks-addr", "127.0.0.1:1080", "upstream sing-box SOCKS5 address")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return &serveOptions{
		SingBox:   *singBox,
		CfgPath:   *cfgPath,
		WorkDir:   *workDir,
		SNIAddr:   *sniAddr,
		HTTPAddr:  *httpAddr,
		SocksAddr: *socksAddr,
	}, nil
}

// ProbePorts reports whether the two loopback listeners sing-box needs to
// hijack are free. Runs BEFORE anything mutates hosts so a port conflict
// doesn't black-hole Kiro traffic.
//
// Exposed so enable.go can call it pre-install and surface a useful error
// instead of silently proceeding.
func ProbePorts(sniAddr, httpAddr string) error {
	if err := probeBind(sniAddr); err != nil {
		return fmt.Errorf("port %s busy (probably HTTPS listener like IIS/HyperV excluded range): %w", sniAddr, err)
	}
	if httpAddr != "" {
		if err := probeBind(httpAddr); err != nil {
			return fmt.Errorf("port %s busy: %w", httpAddr, err)
		}
	}
	return nil
}

// probeBind does a one-shot listen+close to test whether a TCP address is
// available. A failure here at enable-time becomes a useful error instead of
// a silent black-hole at runtime.
func probeBind(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return l.Close()
}

// serveCore is the actual worker loop: spawn sing-box, start the HTTP hijack
// goroutine, run the SNI proxy. Blocks until ctx is cancelled or a proxy
// listener dies. Platform entry points own lifecycle (signals on Unix,
// SCM messages on Windows) and pass a cancellable context in.
func serveCore(ctx context.Context, opts *serveOptions) error {
	// 1. Spawn sing-box.
	sb := exec.CommandContext(ctx, opts.SingBox, "-D", opts.WorkDir, "run", "-c", opts.CfgPath)
	sb.Stdout = os.Stdout
	sb.Stderr = os.Stderr
	if err := sb.Start(); err != nil {
		return fmt.Errorf("start sing-box: %w", err)
	}
	log.Printf("sing-box pid=%d", sb.Process.Pid)

	// 2. Give sing-box a moment to bind its SOCKS5 port.
	select {
	case <-ctx.Done():
		_ = sb.Process.Kill()
		return ctx.Err()
	case <-time.After(300 * time.Millisecond):
	}

	// 3. HTTP hijack (best-effort, in case Kiro ever falls back to plain HTTP).
	if opts.HTTPAddr != "" {
		go func() {
			srv := &sni.Server{Addr: opts.HTTPAddr, SocksAddr: opts.SocksAddr}
			if err := srv.Run(); err != nil {
				log.Printf("http hijack: %v", err)
			}
		}()
	}

	// 4. Main SNI proxy. Runs on the caller's goroutine so Run() returning
	// surfaces any listener error as our return value.
	errc := make(chan error, 1)
	go func() {
		srv := &sni.Server{Addr: opts.SNIAddr, SocksAddr: opts.SocksAddr}
		errc <- srv.Run()
	}()

	select {
	case <-ctx.Done():
		_ = sb.Process.Signal(interruptSignal())
		// Give sing-box 2s to shut down gracefully before we kill it.
		done := make(chan struct{})
		go func() { _, _ = sb.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = sb.Process.Kill()
		}
		return ctx.Err()
	case err := <-errc:
		// SNI listener died. Take sing-box down with us.
		_ = sb.Process.Kill()
		return err
	}
}
