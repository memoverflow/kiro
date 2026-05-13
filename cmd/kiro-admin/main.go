// kiro-admin — EC2-side multi-user management Web UI.
//
// Binds to 127.0.0.1 only. Reach it from your Mac via SSH port forwarding:
//
//	ssh -L 8080:127.0.0.1:8080 ubuntu@<EIP>
//	open http://127.0.0.1:8080/
//
// Storage layout:
//
//	/etc/kiro-admin/users.json      Shadowsocks users
//	/etc/kiro-admin/server.key      outer PSK
//	/etc/kiro-admin/admin.json      Web admin bcrypt hash
//	/etc/kiro-admin/audit.jsonl     admin action log
//
// The binary itself is self-contained: embedded templates, stdlib only except
// golang.org/x/crypto/bcrypt for the admin password.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/xrre/kiro-proxy/pkg/admin"
)

func main() {
	var (
		listen     = flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
		stateDir   = flag.String("state", "/etc/kiro-admin", "directory holding users.json, server.key, audit.jsonl, admin.json")
		configPath = flag.String("config", "/etc/sing-box/config.json", "path sing-box reads (rendered from users.json)")
		eip        = flag.String("eip", "", "public EC2 IP used in generated client env files")
		initAdmin  = flag.Bool("init-admin", false, "prompt for an admin username/password, write admin.json, then exit")
	)
	flag.Parse()

	if *initAdmin {
		if err := doInitAdmin(*stateDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *eip == "" {
		log.Fatalln("need -eip <public-ip> (used when generating client env files)")
	}

	store, err := admin.NewStore(*stateDir)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	auditLog, err := admin.NewAuditLog(*stateDir + "/audit.jsonl")
	if err != nil {
		log.Fatalf("init audit: %v", err)
	}
	cred, err := admin.LoadAdminCred(*stateDir + "/admin.json")
	if err != nil {
		log.Fatalf("load admin cred: %v (run with -init-admin first)", err)
	}

	// Render the sing-box config once at startup so systemd can (re)start
	// sing-box with the current users if someone restarted the box.
	if err := admin.RenderSingBoxConfig(store, *configPath); err != nil {
		log.Printf("warn: could not render %s at startup: %v", *configPath, err)
	}

	srv, err := admin.NewServer(admin.ServerConfig{
		Store:      store,
		Audit:      auditLog,
		AdminCred:  cred,
		ConfigPath: *configPath,
		EIP:        *eip,
	})
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	log.Printf("kiro-admin listening on %s (state=%s)", *listen, *stateDir)
	if err := http.ListenAndServe(*listen, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

func doInitAdmin(stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", stateDir, err)
	}

	user := os.Getenv("KIRO_ADMIN_USER")
	pass := os.Getenv("KIRO_ADMIN_PASS")
	if user == "" || pass == "" {
		fmt.Print("admin username: ")
		_, _ = fmt.Scanln(&user)
		fmt.Print("admin password: ")
		_, _ = fmt.Scanln(&pass)
	}
	if user == "" || pass == "" {
		return fmt.Errorf("username and password required")
	}
	if len(pass) < 8 {
		return fmt.Errorf("password too short (<8 chars)")
	}

	path := stateDir + "/admin.json"
	if err := admin.WriteAdminCred(path, user, pass); err != nil {
		return err
	}
	fmt.Printf("✓ wrote %s\n", path)
	return nil
}
