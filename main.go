// Command pd-status-wall runs a single-binary status video wall for
// PagerDuty Business Services: a background poller, the public display
// page, and an admin panel, all served from one process with embedded
// static assets. See README.md for configuration and deployment.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
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

	// Claim the port before anything else. Generating a password first and
	// failing to bind afterwards prints credentials for a process that
	// never serves, and leaves behind a data directory nobody can use -
	// easy to miss, because the failure is one line under a large banner.
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			log.Fatalf("cannot start: %s is already in use.\n"+
				"Something else is on that port - very likely another copy of this app.\n"+
				"Stop it, or pick another port:  LISTEN_ADDR=127.0.0.1:8090 pd-status-wall",
				listenAddr)
		}
		log.Fatalf("cannot listen on %s: %v", listenAddr, err)
	}
	defer listener.Close()

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

	// Serving on the listener claimed above, so "listening on" is only ever
	// printed once the port is genuinely held.
	url := displayURL(listenAddr, tlsEnabled)
	log.Printf("pd-status-wall listening on %s", url)
	log.Printf("  display: %s", url)
	log.Printf("  admin:   %s/admin", url)

	go func() {
		var err error
		if tlsEnabled {
			err = httpServer.ServeTLS(listener, tlsCertFile, tlsKeyFile)
		} else {
			err = httpServer.Serve(listener)
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	openBrowser(url)

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

// displayURL turns a bind address into something a browser can open.
// A wildcard bind (0.0.0.0 / ::) isn't a reachable address, so it becomes
// localhost.
func displayURL(listenAddr string, tls bool) string {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return scheme + "://" + listenAddr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// openBrowser opens the display once the server is up, which is what you
// want on a laptop and emphatically not what you want on a server.
//
// It stays quiet when there is nothing to open onto: under systemd, over
// SSH, or on a Linux box with no graphical session. OPEN_BROWSER=false
// disables it outright; OPEN_BROWSER=true forces it past these checks.
func openBrowser(url string) {
	switch strings.ToLower(os.Getenv("OPEN_BROWSER")) {
	case "false", "0", "no":
		return
	case "true", "1", "yes":
	default:
		if os.Getenv("INVOCATION_ID") != "" { // started by systemd
			return
		}
		if os.Getenv("SSH_CONNECTION") != "" { // remote shell, no local screen
			return
		}
		if runtime.GOOS == "linux" &&
			os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return // headless
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	// Best effort: failing to open a browser is never a reason to stop
	// serving, so the error is reported and otherwise ignored.
	if err := cmd.Start(); err != nil {
		log.Printf("could not open a browser automatically (%v) - open %s yourself", err, url)
		return
	}
	go func() { _ = cmd.Wait() }() // don't leave a zombie behind
}
