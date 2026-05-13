//go:build windows

package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
)

// defaultSingBoxDev is the dev fallback when a human runs `kiroctl serve`
// directly. The service registration always passes an explicit path.
const defaultSingBoxDev = `C:\Program Files\sing-box\sing-box.exe`

// interruptSignal is the graceful-shutdown hint for the sing-box child. On
// Windows there's no SIGTERM; os.Interrupt translates to CTRL_BREAK_EVENT
// through the Go runtime. sing-box handles it.
func interruptSignal() os.Signal { return os.Interrupt }

// platformServe detects whether we were started by the Windows SCM or by a
// human in a console and picks the right mode:
//
//   - SCM → svc.Run() to do the SCM handshake (StartPending → Running →
//     StopPending). Without this, SCM kills us with error 1053 after the
//     start timeout.
//   - Console → run serveCore directly, like macOS. Useful for `kiroctl serve`
//     at a PowerShell prompt during dev.
func platformServe(opts *serveOptions) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("detect service mode: %w", err)
	}
	if !isService {
		// Interactive run. Just serve; Ctrl+C translates to os.Interrupt.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		return serveCore(ctx, opts)
	}

	h := &serviceHandler{opts: opts}
	return svc.Run(ServiceName, h)
}

// serviceHandler implements golang.org/x/sys/windows/svc.Handler. It reports
// state transitions back to SCM and translates Stop/Shutdown control requests
// into context cancellation for serveCore.
type serviceHandler struct {
	opts *serveOptions
}

// Execute is called by svc.Run on its own goroutine after the SCM handshake.
// We must post StartPending immediately, then Running once we're actually
// serving, then StopPending when asked to stop.
//
// Accepted controls: Stop and Shutdown (the latter fires on system shutdown).
func (h *serviceHandler) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (ssec bool, errno uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	// 1. Tell SCM we heard it and are starting up. Without this the 30s
	// service start timer keeps running.
	s <- svc.Status{State: svc.StartPending}

	// 2. Kick off the real work on a background goroutine. serveCore blocks
	// until ctx is cancelled or a listener dies, so we can't call it inline
	// — we have to stay in Execute() to drain the control channel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errc := make(chan error, 1)
	go func() { errc <- serveCore(ctx, h.opts) }()

	// 3. Flip to Running. From here SCM shows "Running" in services.msc and
	// any dependent services can start.
	s <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				// Echo current status so SCM can poll us for health.
				s <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Printf("service: received %v, stopping", c.Cmd)
				s <- svc.Status{State: svc.StopPending}
				cancel()
				// Wait up to 10s for serveCore to wind down. Longer
				// than that and SCM may start flagging us as stuck.
				select {
				case <-errc:
				case <-time.After(10 * time.Second):
					log.Printf("service: serveCore didn't exit in time, forcing")
				}
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
				log.Printf("service: unexpected control %v", c.Cmd)
			}
		case err := <-errc:
			// serveCore died on its own (listener error, sing-box crash).
			// Report and exit.
			if err != nil {
				log.Printf("service: serveCore error: %v", err)
				s <- svc.Status{State: svc.Stopped}
				return false, windowsServiceError
			}
			s <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}

// windowsServiceError is the errno we hand the SCM when Execute returns
// because of a serveCore error. 1 is the Windows "general failure" code —
// services.msc shows this as "the service terminated unexpectedly".
const windowsServiceError uint32 = 1
