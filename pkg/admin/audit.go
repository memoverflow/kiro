package admin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// AuditEvent records an administrative action for after-the-fact review.
type AuditEvent struct {
	Time   time.Time `json:"time"`
	Actor  string    `json:"actor"`          // admin login name
	Action string    `json:"action"`         // user.create, user.delete, key.rotate, auth.login, ...
	Target string    `json:"target"`         // affected user name (if any)
	IP     string    `json:"ip"`             // requester IP
	Note   string    `json:"note,omitempty"` // free-form
	Ok     bool      `json:"ok"`             // outcome
}

// AuditLog is an append-only JSONL file.
type AuditLog struct {
	path string
	mu   sync.Mutex
}

// NewAuditLog creates the log file if missing.
func NewAuditLog(path string) (*AuditLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	_ = f.Close()
	return &AuditLog{path: path}, nil
}

// Record appends one event.
func (l *AuditLog) Record(e AuditEvent) error {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, string(b))
	return err
}

// Tail returns the last N events, newest last.
func (l *AuditLog) Tail(n int) ([]AuditEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Simple approach: read the whole file. Audit volume is low; tail files
	// are rotated via logrotate externally if needed.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)

	var all []AuditEvent
	for scanner.Scan() {
		var e AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue // skip garbage lines, don't fail the whole read
		}
		all = append(all, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}
