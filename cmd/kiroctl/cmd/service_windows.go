//go:build windows

package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// ServiceName is the Windows Service identifier. Windows service names are
// case-insensitive and cannot contain slashes or backslashes; we pick a
// short, kiro-friendly alias rather than reusing the macOS bundle id.
const ServiceName = "Kiroctl"

// ServiceDisplay is what shows up in services.msc.
const ServiceDisplay = "Kiroctl (Kiro IDE proxy)"

// InstallService registers a Windows service that runs `kiroctl serve`.
// Idempotent: removes any previous registration before creating the new one
// so the binary path + args are always up-to-date.
func InstallService(kiroctlPath, singBoxPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	// Drop any stale registration first.
	if s, err := m.OpenService(ServiceName); err == nil {
		_, _ = s.Control(svc.Stop)
		_ = s.Delete()
		s.Close()
		// Give the SCM a moment to actually forget us.
		time.Sleep(500 * time.Millisecond)
	}

	s, err := m.CreateService(ServiceName, kiroctlPath, mgr.Config{
		DisplayName:      ServiceDisplay,
		Description:      "Lock Kiro IDE/CLI traffic to an EC2 egress via Shadowsocks 2022.",
		StartType:        mgr.StartManual, // user controls via `kiroctl enable/disable`
		ServiceStartName: "LocalSystem",
	},
		"serve",
		"-sing-box", singBoxPath,
		"-config", SystemConfigPath(),
		"-workdir", WorkDirSystem,
		"-sni-addr", "127.0.0.1:443",
		"-http-addr", "127.0.0.1:80",
		"-socks-addr", "127.0.0.1:1080",
	)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	// mgr.Start() only hands the start request to SCM — it doesn't wait for
	// the service to actually report Running. Without this poll we'd return
	// "success" while the service is still StartPending (or has already
	// failed and is in Stopped with a non-zero exit code).
	return waitUntilRunning(s, 15*time.Second)
}

// waitUntilRunning polls the service every 250ms until it reaches Running or
// definitively fails (Stopped with a win32 exit code). Timeout is bounded so
// we never hang install/enable forever.
func waitUntilRunning(s *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		st, err := s.Query()
		if err != nil {
			return fmt.Errorf("query after start: %w", err)
		}
		switch st.State {
		case svc.Running:
			return nil
		case svc.Stopped:
			// If the service hit Execute() and returned an error we'll see
			// a non-zero Win32ExitCode here.
			if st.Win32ExitCode != 0 {
				return fmt.Errorf("service stopped with win32 exit code %d", st.Win32ExitCode)
			}
			return fmt.Errorf("service stopped before reaching Running state")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service did not reach Running state within %s (last state: %s)", timeout, stateName(st.State))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// StopService issues Stop and deletes the service entry. Best effort.
func StopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return nil // SCM unreachable → nothing to stop.
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return nil
	}
	defer s.Close()

	_, _ = s.Control(svc.Stop)
	// Don't delete here — `kiroctl disable` should be reversible by just
	// flipping the service back on. Use `kiroctl uninstall` (future) for full
	// removal. For now we just stop.
	return nil
}

// ServiceStatus returns a short status line: state + startup + binary path.
func ServiceStatus() string {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Sprintf("service manager: %v", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return "service not installed"
	}
	defer s.Close()

	st, err := s.Query()
	if err != nil {
		return fmt.Sprintf("query: %v", err)
	}
	return fmt.Sprintf("state=%s pid=%d", stateName(st.State), st.ProcessId)
}

func stateName(s svc.State) string {
	switch s {
	case svc.Stopped:
		return "Stopped"
	case svc.StartPending:
		return "StartPending"
	case svc.StopPending:
		return "StopPending"
	case svc.Running:
		return "Running"
	case svc.ContinuePending:
		return "ContinuePending"
	case svc.PausePending:
		return "PausePending"
	case svc.Paused:
		return "Paused"
	}
	return fmt.Sprintf("%d", s)
}

// FlushDNS kicks ipconfig. Windows doesn't have a separate mDNS resolver to
// SIGHUP so this is the one command we need.
func FlushDNS() {
	out, err := exec.Command("ipconfig", "/flushdns").CombinedOutput()
	if err != nil && !strings.Contains(string(out), "Successfully") {
		// best effort; don't fail the caller
		return
	}
}
