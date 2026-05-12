//go:build windows

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows-specific paths. We put state under ProgramData (machine-wide,
// writable only by admins) to mirror macOS /Library/Application Support.
var (
	// WorkDirSystem is populated at init because %ProgramData% isn't a
	// constant. Typical value: C:\ProgramData\KiroProxy
	WorkDirSystem string
	// PlistPath is unused on Windows but kept so cross-platform status.go
	// can reference it for logging. We repoint it at the service name file.
	PlistPath string
	LogOut    string
	LogErr    string
)

const singBoxFilename = "sing-box.exe"

func init() {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	WorkDirSystem = filepath.Join(base, "KiroProxy")
	PlistPath = filepath.Join(WorkDirSystem, "service.registered")
	LogOut = filepath.Join(WorkDirSystem, "logs", "kiroproxy.out.log")
	LogErr = filepath.Join(WorkDirSystem, "logs", "kiroproxy.err.log")
}

// isElevated reports whether the current process has the Administrators
// membership / privileges needed to install services and write ProgramData.
func isElevated() bool {
	var token windows.Token
	proc := windows.CurrentProcess()
	if err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

// mustElevate re-launches the current process with UAC elevation if needed.
// On success the parent exits; on failure we surface the error to stderr.
func mustElevate() error {
	if isElevated() {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find self: %w", err)
	}

	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := syscall.UTF16PtrFromString(self)
	cwd, _ := os.Getwd()
	cwdPtr, _ := syscall.UTF16PtrFromString(cwd)
	args, _ := syscall.UTF16PtrFromString(quoteArgs(os.Args[1:]))

	// SW_NORMAL = 1
	if err := windowsShellExecute(verb, exe, args, cwdPtr, 1); err != nil {
		return fmt.Errorf("UAC elevation failed (ShellExecute): %w", err)
	}
	// The elevated child runs in a new console — parent exits immediately.
	fmt.Fprintln(os.Stderr, "(a UAC prompt was raised; output continues in the elevated window)")
	os.Exit(0)
	return nil
}

// quoteArgs re-quotes argv for ShellExecute's lpParameters, which takes a
// single command-line string rather than argv. Simple: wrap anything with
// whitespace in double quotes.
func quoteArgs(in []string) string {
	var b strings.Builder
	for i, a := range in {
		if i > 0 {
			b.WriteByte(' ')
		}
		if strings.ContainsAny(a, " \t\"") {
			b.WriteByte('"')
			b.WriteString(strings.ReplaceAll(a, `"`, `\"`))
			b.WriteByte('"')
		} else {
			b.WriteString(a)
		}
	}
	return b.String()
}

// windowsShellExecute wraps ShellExecuteW with minimal error surface.
func windowsShellExecute(verb, file, params, dir *uint16, show int32) error {
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteW")
	ret, _, err := proc.Call(
		0, // hwnd
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(show),
	)
	// ShellExecuteW returns a HINSTANCE > 32 on success.
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW returned %d: %w", ret, err)
	}
	return nil
}
