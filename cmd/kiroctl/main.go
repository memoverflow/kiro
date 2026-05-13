// kiroctl — control plane for the Mac-side Kiro access restrictor.
//
// Commands:
//
//	kiroctl enable     lock Kiro domains to localhost and start sing-box
//	kiroctl disable    unlock domains and stop sing-box
//	kiroctl status     show current state
//	kiroctl dashboard  open the sing-box Clash UI in a browser
//
// This binary is a thin dispatcher. The heavy lifting (hosts editing and
// sing-box process management) runs under sudo. During installation the
// installer writes a /etc/sudoers.d/kiroctl file that lets the caller run
// the bundled sing-box + /bin/sh (for restarting the launchd plist) without
// a password prompt.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xrre/kiro-proxy/cmd/kiroctl/cmd"
)

func main() {
	flag.Usage = usage
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	sub := os.Args[1]
	args := os.Args[2:]

	switch sub {
	case "enable":
		mustRun(cmd.Enable(args))
	case "disable":
		mustRun(cmd.Disable(args))
	case "status":
		mustRun(cmd.Status(args))
	case "dashboard":
		mustRun(cmd.Dashboard(args))
	case "serve":
		mustRun(cmd.Serve(args))
	case "config":
		mustRun(cmd.Config(args))
	case "install":
		mustRun(cmd.Install(args))
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %q\n\n", sub)
		usage()
		os.Exit(2)
	}
}

func mustRun(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "kiroctl: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `kiroctl — force Kiro IDE/CLI traffic through an EC2 hop.

Usage:
  kiroctl install      one-shot bootstrap: copy self into /usr/local/bin,
                       extract embedded sing-box, write sudoers rule
  kiroctl enable       lock Kiro domains to 127.0.0.1 and start the tunnel
  kiroctl disable      restore hosts and stop the tunnel
  kiroctl status       show current state
  kiroctl dashboard    open the sing-box web dashboard
  kiroctl config …     manage client contexts (kubectl-style)
  kiroctl help         this message

Environment:
  KIRO_CONFIG          path to client config.json (default: ~/.kiro-proxy/config.json)
  KIRO_PROXY_ENV       legacy env file path (default: ~/.kiro-proxy/config.env)
  KIRO_SING_BOX        path to sing-box binary (default: sing-box in PATH)
`)
}
