// Package hosts manages a bounded region inside /etc/hosts that maps Kiro
// domains to 127.0.0.1 (intercepted by a local sing-box).
//
// The region is delimited by markers so we can non-destructively add/remove
// our entries without touching anything else in the file:
//
//	# >>> kiroctl managed block (do not edit manually)
//	127.0.0.1 app.kiro.dev
//	...
//	# <<< kiroctl managed block
package hosts

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

const (
	HostsPath   = "/etc/hosts"
	BlockStart  = "# >>> kiroctl managed block (do not edit manually)"
	BlockEnd    = "# <<< kiroctl managed block"
	InterceptIP = "127.0.0.1"
)

// IsInstalled reports whether the kiroctl block is currently present.
func IsInstalled() (bool, error) {
	b, err := os.ReadFile(HostsPath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", HostsPath, err)
	}
	return bytes.Contains(b, []byte(BlockStart)), nil
}

// Install writes the kiroctl block with the given domains. It is idempotent:
// an existing block is replaced in place.
func Install(domains []string) error {
	orig, err := os.ReadFile(HostsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", HostsPath, err)
	}

	newContent := replaceBlock(orig, buildBlock(domains))
	if bytes.Equal(orig, newContent) {
		return nil
	}
	return writeHosts(newContent)
}

// Uninstall removes the kiroctl block if present.
func Uninstall() error {
	orig, err := os.ReadFile(HostsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", HostsPath, err)
	}
	newContent := replaceBlock(orig, nil)
	if bytes.Equal(orig, newContent) {
		return nil
	}
	return writeHosts(newContent)
}

// ListInstalled returns the domains currently in the managed block.
func ListInstalled() ([]string, error) {
	b, err := os.ReadFile(HostsPath)
	if err != nil {
		return nil, err
	}
	lines := extractBlock(b)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			out = append(out, fields[1])
		}
	}
	return out, nil
}

// ─── internals ──────────────────────────────────────────────────────────

func buildBlock(domains []string) []byte {
	var buf bytes.Buffer
	buf.WriteString(BlockStart)
	buf.WriteByte('\n')
	for _, d := range domains {
		fmt.Fprintf(&buf, "%s %s\n", InterceptIP, d)
	}
	buf.WriteString(BlockEnd)
	buf.WriteByte('\n')
	return buf.Bytes()
}

// replaceBlock splices the new managed block into orig. If newBlock is nil,
// the block is removed. If the block does not exist, it is appended.
func replaceBlock(orig, newBlock []byte) []byte {
	scanner := bufio.NewScanner(bytes.NewReader(orig))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var out bytes.Buffer
	inBlock := false
	sawBlock := false

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.TrimSpace(line) == BlockStart:
			sawBlock = true
			inBlock = true
			continue
		case inBlock && strings.TrimSpace(line) == BlockEnd:
			inBlock = false
			continue
		case inBlock:
			continue
		default:
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}

	// Trim trailing blank lines introduced by removing the block.
	trimmed := bytes.TrimRight(out.Bytes(), "\n")
	out.Reset()
	out.Write(trimmed)
	if trimmed != nil {
		out.WriteByte('\n')
	}

	if len(newBlock) > 0 {
		if !sawBlock && out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.Write(newBlock)
	}

	return out.Bytes()
}

func extractBlock(orig []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(orig))
	var out []string
	inBlock := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == BlockStart {
			inBlock = true
			continue
		}
		if trimmed == BlockEnd {
			inBlock = false
			continue
		}
		if inBlock {
			out = append(out, line)
		}
	}
	return out
}

func writeHosts(content []byte) error {
	tmp := HostsPath + ".kiroctl.tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, HostsPath); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	// macOS: flush DNS cache so changes take effect immediately.
	// We don't error on failure — worst case is a few seconds of stale cache.
	return nil
}
