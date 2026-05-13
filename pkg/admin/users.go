// Package admin is the EC2-side multi-user management layer:
//
//   - users.go    CRUD for Shadowsocks users stored in a JSON file
//   - audit.go    append-only JSONL event log
//   - singbox.go  render /etc/sing-box/config.json from users + server-key
//   - reload.go   safely reload sing-box systemd service
//   - server.go   HTTP UI (bound to localhost; reach via SSH tunnel)
//
// All persistence lives under /etc/kiro-admin/ (root-owned).
package admin

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

var (
	// ErrNotFound surfaces when a user name isn't known.
	ErrNotFound = errors.New("user not found")
	// ErrExists surfaces on duplicate creation.
	ErrExists = errors.New("user already exists")

	validName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)
)

// User is one Shadowsocks 2022 identity.
type User struct {
	Name      string    `json:"name"`
	Password  string    `json:"password"`
	CreatedAt time.Time `json:"created_at"`
	Note      string    `json:"note,omitempty"`
}

// Store is a file-backed user database. All access is serialised by a
// single mutex; concurrent writes would otherwise clobber the JSON.
type Store struct {
	dir       string
	usersPath string
	keyPath   string

	mu sync.Mutex
}

// NewStore opens or creates the database.
func NewStore(dir string) (*Store, error) {
	s := &Store{
		dir:       dir,
		usersPath: filepath.Join(dir, "users.json"),
		keyPath:   filepath.Join(dir, "server.key"),
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if _, err := os.Stat(s.usersPath); os.IsNotExist(err) {
		if err := s.writeUsers(nil); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(s.keyPath); os.IsNotExist(err) {
		key, err := randomKey()
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(s.keyPath, []byte(key), 0o600); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ServerKey returns the shared server PSK used as the outer Shadowsocks 2022
// password. Clients concatenate it with their per-user key: "server:user".
func (s *Store) ServerKey() (string, error) {
	b, err := os.ReadFile(s.keyPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// List returns all users, sorted by name.
func (s *Store) List() ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readUsers()
}

// Get fetches one user.
func (s *Store) Get(name string) (User, error) {
	users, err := s.List()
	if err != nil {
		return User{}, err
	}
	for _, u := range users {
		if u.Name == name {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

// Create adds a user with a freshly generated PSK. Returns the created user.
func (s *Store) Create(name, note string) (User, error) {
	if !validName.MatchString(name) {
		return User{}, fmt.Errorf("invalid name: %s", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	users, err := s.readUsers()
	if err != nil {
		return User{}, err
	}
	for _, u := range users {
		if u.Name == name {
			return User{}, ErrExists
		}
	}
	pass, err := randomKey()
	if err != nil {
		return User{}, err
	}
	u := User{
		Name:      name,
		Password:  pass,
		CreatedAt: time.Now().UTC(),
		Note:      note,
	}
	users = append(users, u)
	if err := s.writeUsers(users); err != nil {
		return User{}, err
	}
	return u, nil
}

// Delete removes the named user. Returns ErrNotFound if absent.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	users, err := s.readUsers()
	if err != nil {
		return err
	}
	idx := -1
	for i, u := range users {
		if u.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ErrNotFound
	}
	users = append(users[:idx], users[idx+1:]...)
	return s.writeUsers(users)
}

// RotateServerKey generates a fresh server PSK. ALL clients have to reconfigure.
func (s *Store) RotateServerKey() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := randomKey()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(s.keyPath, []byte(key), 0o600); err != nil {
		return "", err
	}
	return key, nil
}

// ─── internals ──────────────────────────────────────────────────────

func (s *Store) readUsers() ([]User, error) {
	b, err := os.ReadFile(s.usersPath)
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	var users []User
	if err := json.Unmarshal(b, &users); err != nil {
		return nil, fmt.Errorf("parse users: %w", err)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })
	return users, nil
}

func (s *Store) writeUsers(users []User) error {
	if users == nil {
		users = []User{}
	}
	b, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.usersPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.usersPath)
}

func randomKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
