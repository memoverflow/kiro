//go:build darwin

package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// defaultSingBoxDev is the dev fallback for `-sing-box` on this platform.
// launchd always passes an explicit path, so this only matters when a
// human runs `kiroctl serve` directly.
const defaultSingBoxDev = "/opt/homebrew/bin/sing-box"

// interruptSignal is the graceful-shutdown signal for the sing-box child.
func interruptSignal() os.Signal { return syscall.SIGTERM }

// platformServe is the macOS entry point for `kiroctl serve`. launchd runs us
// as a plain long-lived process; we just need to translate SIGTERM/SIGINT
// into context cancellation.
func platformServe(opts *serveOptions) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		log.Printf("signal received, shutting down")
		cancel()
	}()

	return serveCore(ctx, opts)
}
