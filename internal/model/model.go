// Package model holds the data types shared across the config store,
// the PagerDuty poller, and the HTTP server.
package model

import "time"

// Status is the normalized state used to render a node on the display page.
type Status string

const (
	StatusOperational Status = "operational"
	StatusWarning     Status = "warning"
	StatusCritical    Status = "critical"
	StatusMaintenance Status = "maintenance"
	StatusDisabled    Status = "disabled"
	// StatusImpacted is used for business services, where PagerDuty only
	// exposes a computed impacted/not_impacted boolean rather than a
	// five-state status.
	StatusImpacted Status = "impacted"
)

// Button is a configurable label+URL pair rendered on the display page.
type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Config is the full persisted application configuration. It is stored as a
// single JSON file with 0600 permissions (see internal/store).
type Config struct {
	// PagerDuty connection
	PDAPIKey            string   `json:"pd_api_key"`
	PDRegion            string   `json:"pd_region"` // "us" | "eu"
	StatusPageID        string   `json:"status_page_id"`
	StatusPageName      string   `json:"status_page_name"`
	ServiceOrder        []string `json:"service_order"` // business_service IDs, display order
	PollIntervalSeconds int      `json:"poll_interval_seconds"`

	// Admin auth
	AdminUsername     string `json:"admin_username"`
	AdminPasswordHash string `json:"admin_password_hash"`

	// Branding
	LogoPath      string `json:"logo_path"`
	BannerPath    string `json:"banner_path"`
	BannerFitMode string `json:"banner_fit_mode"` // "contain" | "cover"
	ThemeMode     string `json:"theme_mode"`      // "color" | "grayscale"
	PrimaryColor  string `json:"primary_color"`
	TextColor     string `json:"text_color"`
	OverallAlign  string `json:"overall_align"` // "left" | "center" - the top status banner's text/icon
	OverallSize   string `json:"overall_size"`  // "small" | "medium" | "large" | "xlarge"
	FontFamily    string `json:"font_family"`   // "system" | "rounded" | "serif" | "monospace"

	// Buttons row
	Buttons []Button `json:"buttons"`

	// Network / security (runtime-editable; bind address and TLS are
	// startup-only flags, see internal/config/flags.go)
	CIDRAllowlist []string `json:"cidr_allowlist"` // CIDR strings; empty = allow all
}

// DefaultConfig returns the configuration used the very first time the app
// starts, before an admin has configured anything.
func DefaultConfig() Config {
	return Config{
		PDRegion:            "us",
		PollIntervalSeconds: 45,
		AdminUsername:       "admin",
		BannerFitMode:       "contain",
		ThemeMode:           "color",
		PrimaryColor:        "#e30000",
		TextColor:           "#1a1a1a",
		OverallAlign:        "left",
		OverallSize:         "medium",
		FontFamily:          "system",
		Buttons:             []Button{},
		CIDRAllowlist:       []string{},
	}
}

// Node is a single row in the display page's tree: a business service or a
// technical service, optionally with children of either kind. The display
// page renders this generically regardless of what the nesting represents.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // "business_service" | "service"
	Status   Status `json:"status"`
	Children []Node `json:"children,omitempty"`
}

// Snapshot is the current, last-known-good state served at /api/state.
type Snapshot struct {
	GeneratedAt    time.Time `json:"generated_at"`
	Overall        Status    `json:"overall"` // operational | impacted | unconfigured
	OverallMessage string    `json:"overall_message"`
	StatusPageName string    `json:"status_page_name"`
	Services       []Node    `json:"services"`
	// LastError is surfaced to the admin panel (never to the public display
	// page) so an admin can tell the poller is failing even though the
	// display keeps showing the last good snapshot.
	LastError  string    `json:"-"`
	LastPollAt time.Time `json:"-"`
	LastPollOK bool      `json:"-"`
}
