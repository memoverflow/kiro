package admin

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// AdminCred is the single admin login stored on disk. One admin per instance
// is enough for a small-team tool; add a second via the CLI if needed.
type AdminCred struct {
	Username   string `json:"username"`
	BcryptHash string `json:"bcrypt_hash"`
}

// LoadAdminCred reads /etc/kiro-admin/admin.json.
func LoadAdminCred(path string) (*AdminCred, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c AdminCred
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse admin.json: %w", err)
	}
	if c.Username == "" || c.BcryptHash == "" {
		return nil, fmt.Errorf("admin.json missing username or bcrypt_hash")
	}
	return &c, nil
}

// Verify returns true if the supplied user+pass matches.
// Constant-time username comparison to avoid timing oracles.
func (c *AdminCred) Verify(user, pass string) bool {
	if subtle.ConstantTimeCompare([]byte(user), []byte(c.Username)) != 1 {
		// Still run bcrypt to keep timing constant.
		_ = bcrypt.CompareHashAndPassword([]byte(c.BcryptHash), []byte(pass))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(c.BcryptHash), []byte(pass)) == nil
}

// WriteAdminCred bcrypts the password and writes the credential file.
func WriteAdminCred(path, user, pass string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	c := AdminCred{Username: user, BcryptHash: string(hash)}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// basicAuthMiddleware gates handlers with HTTP Basic Auth.
func basicAuthMiddleware(cred *AdminCred, realm string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := decodeBasicAuth(r.Header.Get("Authorization"))
		if ok && cred.Verify(user, pass) {
			// Attach actor to context via request header for audit hooks.
			r.Header.Set("X-Admin-Actor", user)
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm=%q`, realm))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func decodeBasicAuth(header string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return "", "", false
	}
	idx := strings.IndexByte(string(decoded), ':')
	if idx < 0 {
		return "", "", false
	}
	return string(decoded[:idx]), string(decoded[idx+1:]), true
}
