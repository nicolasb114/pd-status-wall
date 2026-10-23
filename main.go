// Command pd-status-wall runs a single-binary status video wall for
// PagerDuty Business Services: a background poller, the public display
// page, and an admin panel, all served from one process with embedded
// static assets. See README.md for configuration and deployment.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/nicolasb114/pd-status-wall/internal/httpserver"
	"github.com/nicolasb114/pd-status-wall/internal/model"
	"github.com/nicolasb114/pd-status-wall/internal/pagerduty"
	"github.com/nicolasb114/pd-status-wall/internal/store"
)

// version is set at build time with
// -ldflags="-X main.version=v1.2.3"; released binaries carry the tag.
var version = ""

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// buildVersion prefers the version stamped at build time, falling back to
// whatever the Go toolchain recorded (set for `go install`-ed builds).
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

func printUsage() {
	fmt.Print(`pd-status-wall - a status video wall for PagerDuty Business Services

Usage:
  pd-status-wall              start the server
  pd-status-wall --version    print the version
  pd-status-wall --help       print this message

Everything else is configured from the admin panel or these environment
variables:

  DATA_DIR               where config and uploads live   (default ./data)
  LISTEN_ADDR            address to bind                 (default 0.0.0.0:8080)
  TLS_CERT_FILE          serve HTTPS with this cert      (optional)
  TLS_KEY_FILE           serve HTTPS with this key       (optional)
  RESET_ADMIN_PASSWORD   set to "true" to generate a new admin password

Once running, the video wall is at / and the admin panel at /admin.
Docs: https://github.com/nicolasb114/pd-status-wall
`)
}

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-version", "-v":
			fmt.Printf("pd-status-wall %s\n", buildVersion())
			return
		case "--help", "-help", "-h":
			printUsage()
			return
		}
	}

	dataDir := getenv("DATA_DIR", "./data")
	listenAddr := getenv("LISTEN_ADDR", "0.0.0.0:8080")
	tlsCertFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")
	tlsEnabled := tlsCertFile != "" && tlsKeyFile != ""

	s, err := store.Open(dataDir)
	if err != nil {
		log.Fatalf("failed to open config store: %v", err)
	}

	if os.Getenv("RESET_ADMIN_PASSWORD") == "true" {
		if err := resetAdminPassword(s); err != nil {
			log.Fatalf("failed to reset admin password: %v", err)
		}
	}

	// Ensure there is always a bootstrap admin password so first run isn't
	// locked out: if none is set and no reset was requested, generate one
	// and print it once, same as the reset flow.
	if s.Get().AdminPasswordHash == "" {
		if err := resetAdminPassword(s); err != nil {
			log.Fatalf("failed to generate initial admin password: %v", err)
		}
	}

	poller := pagerduty.NewPoller(s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go poller.Run(ctx)

	srv := httpserver.New(s, tlsEnabled)

	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      srv.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		var err error
		if tlsEnabled {
			log.Printf("pd-status-wall listening on https://%s", listenAddr)
			err = httpServer.ListenAndServeTLS(tlsCertFile, tlsKeyFile)
		} else {
			log.Printf("pd-status-wall listening on http://%s", listenAddr)
			err = httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

// resetAdminPassword generates a random password, hashes it, persists it,
// and prints the plaintext once to stdout - the same forgotten-password
// escape hatch pattern used by tools like Grafana.
func resetAdminPassword(s *store.Store) error {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	password := base64.RawURLEncoding.EncodeToString(raw)

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	err = s.Update(func(cfg *model.Config) {
		if cfg.AdminUsername == "" {
			cfg.AdminUsername = "admin"
		}
		cfg.AdminPasswordHash = string(hash)
	})
	if err != nil {
		return err
	}

	cfg := s.Get()
	log.Printf("==============================================================")
	log.Printf(" Admin password generated. Save it now - it will not be shown again.")
	log.Printf(" Username: %s", cfg.AdminUsername)
	log.Printf(" Password: %s", password)
	log.Printf("==============================================================")
	return nil
}
