package cmd

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/xrre/kiro-proxy/pkg/sni"
)

// Serve is the daemon body launchd runs as root. It starts sing-box as a
// child and runs the SNI proxy in the current goroutine.
//
// This subcommand is not meant to be called by humans directly. Use
// `kiroctl enable` — which writes the plist that invokes us.
func Serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	singBox := fs.String("sing-box", "/opt/homebrew/bin/sing-box", "sing-box binary path")
	cfgPath := fs.String("config", SystemConfigPath(), "sing-box config path")
	workDir := fs.String("workdir", WorkDirSystem, "sing-box working directory")
	sniAddr := fs.String("sni-addr", "127.0.0.1:443", "SNI proxy listen address")
	httpAddr := fs.String("http-addr", "127.0.0.1:80", "plain HTTP hijack listen address (empty to disable)")
	socksAddr := fs.String("socks-addr", "127.0.0.1:1080", "upstream sing-box SOCKS5 address")
	fs.Parse(args)

	// 1. Spawn sing-box.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sb := exec.CommandContext(ctx, *singBox, "-D", *workDir, "run", "-c", *cfgPath)
	sb.Stdout = os.Stdout
	sb.Stderr = os.Stderr
	if err := sb.Start(); err != nil {
		return fmt.Errorf("start sing-box: %w", err)
	}
	log.Printf("sing-box pid=%d", sb.Process.Pid)

	// 2. Wait briefly for sing-box to bind its SOCKS5 port before we
	// start accepting connections on :443.
	time.Sleep(300 * time.Millisecond)

	// 3. SIGTERM handling: propagate to sing-box.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		log.Printf("signal received, shutting down")
		cancel()
		_ = sb.Process.Signal(syscall.SIGTERM)
		time.Sleep(2 * time.Second)
		_ = sb.Process.Kill()
		os.Exit(0)
	}()

	// 4. HTTP hijack in a goroutine (best effort, in case Kiro ever falls
	// back to plain HTTP for a subset of domains — unlikely but cheap to
	// cover).
	if *httpAddr != "" {
		go func() {
			srv := &sni.Server{
				Addr:      *httpAddr,
				SocksAddr: *socksAddr,
			}
			if err := srv.Run(); err != nil {
				log.Printf("http hijack: %v", err)
			}
		}()
	}

	// 5. Main SNI proxy.
	srv := &sni.Server{
		Addr:      *sniAddr,
		SocksAddr: *socksAddr,
	}
	return srv.Run()
}
