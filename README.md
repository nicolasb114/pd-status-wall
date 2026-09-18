# pd-status-wall

A single-binary Go application that displays the current status of
PagerDuty Business Services on a video wall — for internal viewing on an
office floor, a NOC, or anywhere else you want a big, always-on status
display.

It runs entirely on your own network: a background poller pulls status data
from the PagerDuty REST API on an **outbound-only** basis (no webhooks, no
public receiver, no inbound internet exposure required). Branding, layout,
theme, and buttons are fully configurable from a built-in admin panel.

New to this? See [QUICKSTART.md](QUICKSTART.md) for the fast path.

## Contents

- [How it works](#how-it-works)
- [Install](#install)
- [Configuration](#configuration)
- [Usage](#usage)
- [Network & security](#network--security)
- [Deployment](#deployment)
- [Troubleshooting](#troubleshooting)
- [Building from source](#building-from-source)

## How it works

One Go process, one binary, no external database, no reverse proxy required:

1. **Background poller** — a goroutine that calls the PagerDuty REST API on
   a timer (default every 45 seconds) and refreshes an in-memory snapshot of
   current status.
2. **HTTP server**, serving three things from the same binary (static
   assets are embedded via Go's `embed` package — the binary is genuinely
   standalone):
   - `/` — the public display page. No login required.
   - `/api/state` — a small JSON endpoint the display page polls client-side
     every ~30 seconds to refresh without a full page reload.
   - `/admin` — the settings panel, behind a simple login.

If a poll fails (network blip, PagerDuty rate limit, PagerDuty outage), the
display keeps showing the **last known-good snapshot** — it never goes blank
because of a transient error.

Status is computed the way PagerDuty itself computes it: rather than
re-implementing PagerDuty's priority-threshold logic, this app calls
PagerDuty's own `business_services/impacts` endpoint, which already returns
a computed impacted/not-impacted state per business service using your
account's configured threshold.

## Install

Download a prebuilt binary from the
[latest release](https://github.com/nicolasb114/pd-status-wall/releases/latest) —
see [QUICKSTART.md](QUICKSTART.md) for the one-command version, or
[build from source](#building-from-source) if you'd rather compile it
yourself.

## Configuration

Most settings live in the admin panel (`/admin`) and are stored in a local
JSON file. A small number of settings are **startup-only** — they affect how
the process binds to the network, so they're environment variables read once
when the binary starts, not something you'd want to change without a
restart anyway.

| Environment variable | Default | Purpose |
|---|---|---|
| `DATA_DIR` | `./data` | Where `config.json` and uploaded images are stored |
| `LISTEN_ADDR` | `0.0.0.0:8080` | Address/port the HTTP server binds to |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | unset | Set both to serve HTTPS instead of plain HTTP |
| `RESET_ADMIN_PASSWORD` | unset | Set to `true` to force-generate a new admin password on this startup |

Example:

```bash
DATA_DIR=/var/lib/pd-status-wall LISTEN_ADDR=10.0.5.20:8080 ./pd-status-wall
```

`config.json` is created automatically on first run, with `0600`
permissions set by the application itself (not a manual step) — it contains
the PagerDuty API key and the bcrypt hash of the admin password, so treat it
like a secret.

### Forgot the admin password?

Stop the process, start it again with `RESET_ADMIN_PASSWORD=true`, and it
will generate a new random password and print it once to the log — the same
pattern tools like Grafana use.

```bash
RESET_ADMIN_PASSWORD=true ./pd-status-wall
```

## Usage

### 1. Connect PagerDuty

In `/admin` → **PagerDuty Connection**:

- **API key** — use a **read-only** REST API key or scoped OAuth token. See
  [PagerDuty's guide to generating one](https://support.pagerduty.com/main/docs/api-access-keys).
  It only needs read access to services, business services, and status
  pages.
- **Region** — `US` (`api.pagerduty.com`) or `EU`
  (`api.eu.pagerduty.com`), depending on which PagerDuty data-center region
  your account is on. If you're not sure, check with whoever administers
  your PagerDuty account.
- **Poll interval** — how often to refresh, in seconds (default 45,
  minimum 10). PagerDuty's REST API rate limits are respected automatically:
  on a `429` response the poller waits until the rate limit resets rather
  than retrying immediately.

### 2. Pick a status page and services

In **Services & Order**, select which PagerDuty status page to mirror (an
account can have several), then reorder the business services shown on it —
the order here is the order they appear on the display page.

Each business service card is generic and recursive: whatever PagerDuty
returns as that service's supporting services becomes a collapsible child
row, and if one of *those* has its own supporting services (for example, a
single service broken into regional sub-components), that nests too. This
works to any depth without special-casing what the nesting represents.

### 3. Branding

In **Branding & Theme**:

- Upload a logo and a banner image.
- Choose the banner **fit mode**: `contain` (show the whole image,
  may letterbox) or `cover` (fill the space, may crop).
- Toggle **display mode** between full color and grayscale. Grayscale mode
  doesn't rely on color alone to communicate status — each status also gets
  a distinct icon shape (circle/square/triangle/etc.) and an explicit text
  label, so it stays legible in black and white.
- Pick a primary/accent color and a text color. The live preview panel
  updates immediately.

### 4. Buttons

In **Buttons**, add label + URL pairs (e.g. "Report an issue", "Internal
runbook"). They render as simple buttons under the status cards on the
display page — no other behavior attached.

### 5. Security

- **Change password** — requires the current password, a new one, and
  confirmation. The username (`admin`) is fixed by design; this is
  intentionally a single shared login, not a multi-user system.
- **IP allowlist** — see [Network & security](#network--security) below.

Once configured, open the display page (`/`, no login needed) on whatever
screen or video wall player points at this server.

## Network & security

This app is designed to run entirely inside a private network, with **no
inbound exposure to the public internet**. It has no webhook receiver by
design — all PagerDuty communication is outbound polling.

Because this tool can't see or control the network it's deployed on, true
"intranet-only" enforcement has to happen at the infrastructure layer.
Concretely, whoever deploys this should:

- **Bind to a specific internal interface**, not `0.0.0.0`, if the host has
  more than one network interface: `LISTEN_ADDR=10.0.5.20:8080`.
- **Firewall the port** so it's reachable only from the intended internal
  subnet/VLAN, and is not reachable from any internet-facing interface.
- **Never port-forward or NAT this port** on any perimeter firewall or
  router.
- Keep the host itself on an internal-only network segment.

As defense-in-depth on top of that — not a replacement for it — the app
supports an **IP/CIDR allowlist** (`/admin` → Security & Network). When set,
every request to `/` and `/admin` is checked against the configured CIDR
ranges (e.g. `10.0.0.0/8`) and rejected with `403` otherwise. Leave it empty
to allow all sources (the default), which only makes sense if the network
layer is already doing this job.

### TLS

By default the app serves plain HTTP, which is normally fine for an
isolated intranet segment. If your network policy requires encryption even
internally, set `TLS_CERT_FILE` and `TLS_KEY_FILE` to serve HTTPS instead —
otherwise the admin password travels in cleartext on the local network.

### Admin auth

The admin panel uses a single, fixed username (`admin`) with a
bcrypt-hashed password — intentionally simple, no SSO, no per-user accounts,
proportionate to the fact that the API key is read-only and the page is
meant to be intranet-only. Sessions are server-side, cookie-based, and
expire after 12 hours.

## Deployment

No database server, no reverse proxy, and no runtime dependencies are
required — copy the binary to the target machine and run it. A minimal
`systemd` unit:

```ini
[Unit]
Description=PagerDuty status video wall
After=network-online.target

[Service]
ExecStart=/opt/pd-status-wall/pd-status-wall
Environment=DATA_DIR=/var/lib/pd-status-wall
Environment=LISTEN_ADDR=10.0.5.20:8080
Restart=on-failure
User=pd-status-wall

[Install]
WantedBy=multi-user.target
```

Outbound network access needed: HTTPS to `api.pagerduty.com` (US region) or
`api.eu.pagerduty.com` (EU region). No inbound internet access is needed or
wanted — see [Network & security](#network--security).

## Troubleshooting

**The display page says "Not configured yet."**
The poller has no API key or status page set — finish the setup in `/admin`.

**The admin panel shows a poller error but the display page looks fine.**
Expected — the display page always shows the last known-good snapshot, even
while the poller is failing (rate limit, network issue, revoked API key).
Check the error message under **Security & Network → Poller status** in
`/admin`.

**I get a 403 on every page.**
An IP allowlist is configured in **Security & Network** and your client's
address isn't in it. Either add your range or clear the allowlist.

**I forgot the admin password.**
See [Forgot the admin password?](#forgot-the-admin-password) above.

## Building from source

Requires Go 1.22+.

```bash
git clone https://github.com/nicolasb114/pd-status-wall.git
cd pd-status-wall
go build -o pd-status-wall .
./pd-status-wall
```

Cross-compiling for another platform:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o pd-status-wall-linux-amd64 .
```

## License

[MIT](LICENSE)
