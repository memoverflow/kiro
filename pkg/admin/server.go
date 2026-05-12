package admin

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

//go:embed templates/*.html.tmpl
var templatesFS embed.FS

// ServerConfig bundles runtime dependencies.
type ServerConfig struct {
	Store      *Store
	Audit      *AuditLog
	AdminCred  *AdminCred
	ConfigPath string // /etc/sing-box/config.json
	EIP        string // public IP used in generated env files
	Port       int
	Method     string
}

// Server is the HTTP UI.
type Server struct {
	cfg  ServerConfig
	tmpl *template.Template
}

// NewServer parses templates and returns the server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Port == 0 {
		cfg.Port = 1443
	}
	if cfg.Method == "" {
		cfg.Method = "2022-blake3-aes-128-gcm"
	}
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{cfg: cfg, tmpl: tmpl}, nil
}

// Handler wires routes and returns an http.Handler under Basic Auth.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.usersPage)
	mux.HandleFunc("/users/create", s.usersCreate)
	mux.HandleFunc("/users/", s.usersItem)
	mux.HandleFunc("/audit", s.auditPage)
	mux.HandleFunc("/dashboard", s.dashboardPage)
	mux.HandleFunc("/dashboard/log", s.dashboardLog)
	mux.HandleFunc("/rotate", s.rotatePage)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	return basicAuthMiddleware(s.cfg.AdminCred, "kiro-admin", mux)
}

// ─── rendering ──────────────────────────────────────────────────────

type pageData struct {
	Title     string
	Page      string
	Actor     string
	Flash     string
	FlashKind string
	Body      template.HTML
	Users     []User
	Events    []AuditEvent
	LogTail   string
}

// renderPage renders a named body template, then embeds it in layout.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, bodyName string, data pageData) {
	data.Actor = actorOf(r)

	var body bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&body, bodyName, data); err != nil {
		http.Error(w, "render body: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data.Body = template.HTML(body.String())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "render layout: "+err.Error(), http.StatusInternalServerError)
	}
}

// ─── helpers ────────────────────────────────────────────────────────

func actorOf(r *http.Request) string { return r.Header.Get("X-Admin-Actor") }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) audit(r *http.Request, action, target, note string, ok bool) {
	_ = s.cfg.Audit.Record(AuditEvent{
		Actor:  actorOf(r),
		Action: action,
		Target: target,
		IP:     clientIP(r),
		Note:   note,
		Ok:     ok,
	})
}

// applyAndReload writes the config from current store state and kicks sing-box.
func (s *Server) applyAndReload() error {
	if err := RenderSingBoxConfig(s.cfg.Store, s.cfg.ConfigPath); err != nil {
		return err
	}
	return ReloadSingBox(s.cfg.ConfigPath)
}

// ─── routes ─────────────────────────────────────────────────────────

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	users, err := s.cfg.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderPage(w, r, "users_body", pageData{Title: "Users", Page: "users", Users: users})
}

func (s *Server) usersCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	note := strings.TrimSpace(r.FormValue("note"))
	_, err := s.cfg.Store.Create(name, note)
	if err != nil {
		s.audit(r, "user.create", name, err.Error(), false)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.applyAndReload(); err != nil {
		s.audit(r, "user.create", name, "reload: "+err.Error(), false)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "user.create", name, note, true)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) usersItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/users/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	name, verb := parts[0], parts[1]
	switch verb {
	case "env":
		s.usersEnv(w, r, name)
	case "delete":
		s.usersDelete(w, r, name)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) usersEnv(w http.ResponseWriter, r *http.Request, name string) {
	u, err := s.cfg.Store.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	serverKey, err := s.cfg.Store.ServerKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body := fmt.Sprintf(`# kiro-proxy client config for %s
# Generated %s by %s
#
# Install:
#   mkdir -p ~/.kiro-proxy && mv %s.env ~/.kiro-proxy/config.env
#   chmod 600 ~/.kiro-proxy/config.env
#   kiroctl enable

KIRO_EC2_HOST=%s
KIRO_SS_PORT=%d
KIRO_SS_METHOD=%s
KIRO_SS_SERVER_KEY=%s
KIRO_SS_USER_NAME=%s
KIRO_SS_USER_KEY=%s
`,
		u.Name,
		u.CreatedAt.Format("2006-01-02 15:04:05 UTC"),
		actorOf(r),
		u.Name,
		s.cfg.EIP, s.cfg.Port, s.cfg.Method,
		serverKey, u.Name, u.Password,
	)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.env"`, u.Name))
	_, _ = w.Write([]byte(body))
	s.audit(r, "user.env", name, "", true)
}

func (s *Server) usersDelete(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := s.cfg.Store.Delete(name); err != nil {
		s.audit(r, "user.delete", name, err.Error(), false)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.applyAndReload(); err != nil {
		s.audit(r, "user.delete", name, "reload: "+err.Error(), false)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "user.delete", name, "", true)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) auditPage(w http.ResponseWriter, r *http.Request) {
	events, err := s.cfg.Audit.Tail(200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// newest first
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	s.renderPage(w, r, "audit_body", pageData{Title: "Audit", Page: "audit", Events: events})
}

func (s *Server) dashboardPage(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "dashboard_body", pageData{
		Title:   "Dashboard",
		Page:    "dashboard",
		LogTail: tailSingBoxLog(150),
	})
}

func (s *Server) dashboardLog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(tailSingBoxLog(150)))
}

func tailSingBoxLog(lines int) string {
	out, err := exec.Command("journalctl", "-u", "sing-box", "-n", fmt.Sprintf("%d", lines), "--no-pager", "-o", "short").CombinedOutput()
	if err != nil {
		return "journalctl error: " + err.Error() + "\n\n" + string(out)
	}
	return string(out)
}

func (s *Server) rotatePage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if _, err := s.cfg.Store.RotateServerKey(); err != nil {
			s.audit(r, "key.rotate", "", err.Error(), false)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.applyAndReload(); err != nil {
			s.audit(r, "key.rotate", "", "reload: "+err.Error(), false)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.audit(r, "key.rotate", "", "", true)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderPage(w, r, "rotate_body", pageData{Title: "Rotate key", Page: "rotate"})
}
