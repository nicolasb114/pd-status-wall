package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/nicolasb114/pd-status-wall/internal/model"
	"github.com/nicolasb114/pd-status-wall/internal/pagerduty"
)

// createRestricted creates a new file with 0600 permissions, applied by the
// application itself at creation time (uploaded images aren't secrets, but
// this keeps a consistent, restrictive default for everything the app
// writes to disk).
func createRestricted(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- Login / logout ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := s.store.Get()
	if body.Username != cfg.AdminUsername || cfg.AdminPasswordHash == "" {
		time.Sleep(300 * time.Millisecond) // slow down brute-forcing a bit
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cfg.AdminPasswordHash), []byte(body.Password)); err != nil {
		time.Sleep(300 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	s.newSession(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearSession(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Config read (masked) ---

type adminConfigView struct {
	Authenticated       bool           `json:"authenticated"`
	PDAPIKeySet         bool           `json:"pd_api_key_set"`
	PDAPIKeyMasked      string         `json:"pd_api_key_masked"`
	PDRegion            string         `json:"pd_region"`
	StatusPageID        string         `json:"status_page_id"`
	StatusPageName      string         `json:"status_page_name"`
	ServiceOrder        []string       `json:"service_order"`
	PollIntervalSeconds int            `json:"poll_interval_seconds"`
	AdminUsername       string         `json:"admin_username"`
	LogoURL             string         `json:"logo_url,omitempty"`
	BannerURL           string         `json:"banner_url,omitempty"`
	BannerFitMode       string         `json:"banner_fit_mode"`
	ThemeMode           string         `json:"theme_mode"`
	PrimaryColor        string         `json:"primary_color"`
	TextColor           string         `json:"text_color"`
	Buttons             []model.Button `json:"buttons"`
	CIDRAllowlist       []string       `json:"cidr_allowlist"`
	LastPollOK          bool           `json:"last_poll_ok"`
	LastPollAt          string         `json:"last_poll_at,omitempty"`
	LastError           string         `json:"last_error,omitempty"`
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	snap := s.store.Snapshot()

	view := adminConfigView{
		Authenticated:       true,
		PDAPIKeySet:         cfg.PDAPIKey != "",
		PDAPIKeyMasked:      maskSecret(cfg.PDAPIKey),
		PDRegion:            cfg.PDRegion,
		StatusPageID:        cfg.StatusPageID,
		StatusPageName:      cfg.StatusPageName,
		ServiceOrder:        cfg.ServiceOrder,
		PollIntervalSeconds: cfg.PollIntervalSeconds,
		AdminUsername:       cfg.AdminUsername,
		BannerFitMode:       cfg.BannerFitMode,
		ThemeMode:           cfg.ThemeMode,
		PrimaryColor:        cfg.PrimaryColor,
		TextColor:           cfg.TextColor,
		Buttons:             cfg.Buttons,
		CIDRAllowlist:       cfg.CIDRAllowlist,
		LastPollOK:          snap.LastPollOK,
		LastError:           snap.LastError,
	}
	if !snap.LastPollAt.IsZero() {
		view.LastPollAt = snap.LastPollAt.UTC().Format(time.RFC3339)
	}
	if cfg.LogoPath != "" {
		view.LogoURL = "/uploads/" + filepath.Base(cfg.LogoPath)
	}
	if cfg.BannerPath != "" {
		view.BannerURL = "/uploads/" + filepath.Base(cfg.BannerPath)
	}
	if view.ServiceOrder == nil {
		view.ServiceOrder = []string{}
	}
	if view.Buttons == nil {
		view.Buttons = []model.Button{}
	}
	if view.CIDRAllowlist == nil {
		view.CIDRAllowlist = []string{}
	}
	writeJSON(w, http.StatusOK, view)
}

// --- PagerDuty connection settings ---

func (s *Server) handleSavePagerDuty(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey              string `json:"pd_api_key"`
		Region              string `json:"pd_region"`
		PollIntervalSeconds int    `json:"poll_interval_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Region != "us" && body.Region != "eu" {
		writeError(w, http.StatusBadRequest, "pd_region must be 'us' or 'eu'")
		return
	}
	if body.PollIntervalSeconds < 10 {
		body.PollIntervalSeconds = 45
	}

	err := s.store.Update(func(cfg *model.Config) {
		// An empty API key in the request means "leave it unchanged" -
		// the field is masked in the UI, so an untouched field would
		// otherwise wipe out a previously saved key.
		if body.APIKey != "" {
			cfg.PDAPIKey = body.APIKey
		}
		cfg.PDRegion = body.Region
		cfg.PollIntervalSeconds = body.PollIntervalSeconds
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListStatusPages(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	if cfg.PDAPIKey == "" {
		writeError(w, http.StatusBadRequest, "set a PagerDuty API key first")
		return
	}
	client := pagerduty.NewClient(cfg.PDAPIKey, regionFromString(cfg.PDRegion))
	pages, err := client.ListStatusPages(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "PagerDuty request failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status_pages": pages})
}

func (s *Server) handleListStatusPageServices(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	statusPageID := r.URL.Query().Get("status_page_id")
	if statusPageID == "" {
		statusPageID = cfg.StatusPageID
	}
	if cfg.PDAPIKey == "" || statusPageID == "" {
		writeError(w, http.StatusBadRequest, "set an API key and status page first")
		return
	}
	client := pagerduty.NewClient(cfg.PDAPIKey, regionFromString(cfg.PDRegion))
	services, err := client.ListStatusPageServices(r.Context(), statusPageID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "PagerDuty request failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

// --- Status page + business service selection / ordering ---

func (s *Server) handleSaveServiceSelection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StatusPageID   string   `json:"status_page_id"`
		StatusPageName string   `json:"status_page_name"`
		ServiceOrder   []string `json:"service_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := s.store.Update(func(cfg *model.Config) {
		cfg.StatusPageID = body.StatusPageID
		cfg.StatusPageName = body.StatusPageName
		cfg.ServiceOrder = body.ServiceOrder
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Branding ---

func (s *Server) handleSaveBranding(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BannerFitMode string `json:"banner_fit_mode"`
		ThemeMode     string `json:"theme_mode"`
		PrimaryColor  string `json:"primary_color"`
		TextColor     string `json:"text_color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.BannerFitMode != "contain" && body.BannerFitMode != "cover" {
		writeError(w, http.StatusBadRequest, "banner_fit_mode must be 'contain' or 'cover'")
		return
	}
	if body.ThemeMode != "color" && body.ThemeMode != "grayscale" {
		writeError(w, http.StatusBadRequest, "theme_mode must be 'color' or 'grayscale'")
		return
	}
	err := s.store.Update(func(cfg *model.Config) {
		cfg.BannerFitMode = body.BannerFitMode
		cfg.ThemeMode = body.ThemeMode
		if body.PrimaryColor != "" {
			cfg.PrimaryColor = body.PrimaryColor
		}
		if body.TextColor != "" {
			cfg.TextColor = body.TextColor
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

const maxUploadSize = 8 << 20 // 8 MiB

var allowedImageExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true,
}

func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind") // "logo" | "banner"
	if kind != "logo" && kind != "banner" {
		writeError(w, http.StatusBadRequest, "kind must be 'logo' or 'banner'")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid upload (max 8MB)")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedImageExt[ext] {
		writeError(w, http.StatusBadRequest, "unsupported file type")
		return
	}

	randBytes := make([]byte, 8)
	_, _ = rand.Read(randBytes)
	filename := fmt.Sprintf("%s-%s%s", kind, hex.EncodeToString(randBytes), ext)
	destPath := filepath.Join(s.store.UploadsDir(), filename)

	dest, err := createRestricted(destPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dest.Close()
	if _, err := io.Copy(dest, file); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save file")
		return
	}

	err = s.store.Update(func(cfg *model.Config) {
		if kind == "logo" {
			cfg.LogoPath = filename
		} else {
			cfg.BannerPath = filename
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": "/uploads/" + filename})
}

// --- Buttons ---

func (s *Server) handleSaveButtons(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Buttons []model.Button `json:"buttons"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := s.store.Update(func(cfg *model.Config) {
		cfg.Buttons = body.Buttons
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Security: password change ---

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if body.NewPassword != body.ConfirmPassword {
		writeError(w, http.StatusBadRequest, "new password and confirmation do not match")
		return
	}

	cfg := s.store.Get()
	if cfg.AdminPasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(cfg.AdminPasswordHash), []byte(body.CurrentPassword)); err != nil {
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	err = s.store.Update(func(c *model.Config) {
		c.AdminPasswordHash = string(hash)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Security: network (CIDR allowlist) ---

func (s *Server) handleSaveNetwork(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CIDRAllowlist []string `json:"cidr_allowlist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cleaned := make([]string, 0, len(body.CIDRAllowlist))
	for _, c := range body.CIDRAllowlist {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(c); err != nil {
			writeError(w, http.StatusBadRequest, "invalid CIDR: "+c)
			return
		}
		cleaned = append(cleaned, c)
	}
	err := s.store.Update(func(cfg *model.Config) {
		cfg.CIDRAllowlist = cleaned
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
