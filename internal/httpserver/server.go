// Package httpserver wires up the three routes served from the single
// binary: the public display page, its JSON polling endpoint, and the
// admin panel.
package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nicolasb114/pd-status-wall/internal/pagerduty"
	"github.com/nicolasb114/pd-status-wall/internal/store"
)

const sessionCookieName = "pdsw_session"
const sessionTTL = 12 * time.Hour

// Server holds everything the HTTP handlers need.
type Server struct {
	store *store.Store
	mux   *http.ServeMux
	tlsOn bool

	sessMu   sync.Mutex
	sessions map[string]time.Time // token -> expiry
}

func New(s *store.Store, tlsOn bool) *Server {
	srv := &Server{
		store:    s,
		mux:      http.NewServeMux(),
		tlsOn:    tlsOn,
		sessions: map[string]time.Time{},
	}
	srv.routes()
	return srv
}

// Handler returns the CIDR-filtered, top-level HTTP handler for the app.
func (s *Server) Handler() http.Handler {
	return s.withCIDRAllowlist(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleDisplay)
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("GET /uploads/", s.handleUploads)

	s.mux.HandleFunc("GET /admin/", s.handleAdminPage)
	s.mux.HandleFunc("POST /admin/login", s.handleLogin)
	s.mux.HandleFunc("POST /admin/logout", s.handleLogout)

	s.mux.HandleFunc("GET /admin/api/config", s.requireAuth(s.handleGetConfig))
	s.mux.HandleFunc("POST /admin/api/pagerduty", s.requireAuth(s.handleSavePagerDuty))
	s.mux.HandleFunc("GET /admin/api/status-pages", s.requireAuth(s.handleListStatusPages))
	s.mux.HandleFunc("GET /admin/api/status-pages/services", s.requireAuth(s.handleListStatusPageServices))
	s.mux.HandleFunc("POST /admin/api/services", s.requireAuth(s.handleSaveServiceSelection))
	s.mux.HandleFunc("POST /admin/api/branding", s.requireAuth(s.handleSaveBranding))
	s.mux.HandleFunc("POST /admin/api/branding/upload", s.requireAuth(s.handleUploadImage))
	s.mux.HandleFunc("POST /admin/api/buttons", s.requireAuth(s.handleSaveButtons))
	s.mux.HandleFunc("POST /admin/api/security/password", s.requireAuth(s.handleChangePassword))
	s.mux.HandleFunc("POST /admin/api/security/network", s.requireAuth(s.handleSaveNetwork))
}

// --- CIDR allowlist middleware (defense-in-depth on top of network-level
// intranet isolation, which this app cannot itself guarantee) ---

func (s *Server) withCIDRAllowlist(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowlist := s.store.Get().CIDRAllowlist
		if len(allowlist) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		if ip == nil || !ipAllowed(ip, allowlist) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

func ipAllowed(ip net.IP, cidrs []string) bool {
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// --- Session-based auth for /admin ---

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) isAuthenticated(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	expiry, ok := s.sessions[c.Value]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.sessions, c.Value)
		return false
	}
	return true
}

func (s *Server) newSession(w http.ResponseWriter) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)

	s.sessMu.Lock()
	s.sessions[token] = time.Now().Add(sessionTTL)
	s.sessMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.tlsOn,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
}

func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		s.sessMu.Lock()
		delete(s.sessions, c.Value)
		s.sessMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func regionFromString(s string) pagerduty.Region {
	if s == "eu" {
		return pagerduty.RegionEU
	}
	return pagerduty.RegionUS
}
