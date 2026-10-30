# pd-status-wall

A single-binary web app that puts the current status of your PagerDuty
Business Services on a screen, and keeps it there.

It is built for an always-on display — a TV on an office floor, a monitor
in a NOC, a projector in an ops room — rather than for a person at a
laptop. Big type, high contrast, readable from across a room, and no
interaction required: the page refreshes itself and never goes blank.

[![CI](https://github.com/nicolasb114/pd-status-wall/actions/workflows/ci.yml/badge.svg)](https://github.com/nicolasb114/pd-status-wall/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/nicolasb114/pd-status-wall?label=latest%20release)](https://github.com/nicolasb114/pd-status-wall/releases/latest)

## What it's for

PagerDuty already renders a status page. This exists for the cases that
page doesn't cover:

- **Your branding, not PagerDuty's.** Status page branding isn't exposed
  through the API, so a wall that has to carry a company's logo, colours
  and typography can't be built from it. Here all of that is yours to set.
- **It runs inside your network.** No inbound connection is needed or
  accepted. The app reaches *out* to the PagerDuty API and nothing reaches
  in — no webhook receiver, no public endpoint, nothing to expose through
  a firewall.
- **It's built to be left alone.** If a poll fails — network blip, rate
  limit, PagerDuty itself having a bad day — the wall keeps showing the
  last known-good state instead of an error or an empty screen.

## What it shows

The display mirrors how your PagerDuty status page is organised: the
business services configured on that page, each with its own status,
optionally collected under named groups.

One thing PagerDuty does *not* expose through its API is the grouping you
set up when building a status page. There is no field and no endpoint for
it — only the rendered public page carries it, and private pages don't
even do that. So groups are re-created in this app's admin panel instead.
A group takes the worst status among its members, so a single failing
service surfaces at the top level.

## Requirements

**To run it** — nothing. One binary, no runtime, no database, no reverse
proxy. It needs:

- A machine running Linux, macOS or Windows (64-bit Intel/AMD or ARM)
- A **read-only** PagerDuty API key
  ([how to create one](https://support.pagerduty.com/main/docs/api-access-keys))
- At least one PagerDuty **status page** with business services on it
- Outbound HTTPS to `api.pagerduty.com`, or `api.eu.pagerduty.com` if your
  account is in the EU region. Nothing else, and nothing inbound.

**To build it from source** — Go 1.26 or newer.

## Install

Pick the section for your machine. Each is complete on its own.

### Windows

1. Open the
   [latest release](https://github.com/nicolasb114/pd-status-wall/releases/latest)
   and, under *Assets*, download **`pd-status-wall-windows.exe`**.
2. Double-click it.
3. Windows will likely show *"Windows protected your PC"*. That is
   SmartScreen reacting to an application it hasn't seen before, not a
   malware detection. Click **More info**, then **Run anyway**.
4. A console window opens and prints a generated admin password. **Copy it
   now** — it is shown once and never again.
5. Open <http://localhost:8080> for the display, and
   <http://localhost:8080/admin> to configure it.
6. Keep that console window open; closing it stops the app.

### Linux

1. Download and unpack:

   ```bash
   curl -L https://github.com/nicolasb114/pd-status-wall/releases/latest/download/pd-status-wall-linux-x86-64.tar.gz | tar xz
   ```

   On ARM machines (Raspberry Pi, AWS Graviton) use
   `pd-status-wall-linux-arm64.tar.gz` instead. `uname -m` tells you which
   you have: `x86_64` or `aarch64`.

2. Run it:

   ```bash
   ./pd-status-wall
   ```

   No `chmod` is needed — the archive preserves the execute bit — and
   there is no security prompt to clear.

3. Copy the admin password it prints.
4. Open <http://localhost:8080> and <http://localhost:8080/admin>.

For anything long-lived, see [Deployment](#deployment) below rather than
leaving a terminal open.

### macOS — Intel

The smoothest route is Homebrew, which also avoids macOS's download
quarantine entirely:

```bash
brew install nicolasb114/tap/pd-status-wall
pd-status-wall
```

Without Homebrew, `curl` works just as well — it doesn't set the
quarantine flag either:

```bash
curl -L https://github.com/nicolasb114/pd-status-wall/releases/latest/download/pd-status-wall-macos-intel.tar.gz | tar xz
./pd-status-wall
```

Either way, copy the admin password it prints, then open
<http://localhost:8080>.

**If you download through a browser instead**, macOS will refuse to run it
— these builds are not notarized by Apple, so Gatekeeper stops them, often
with no message beyond the process being killed. Clear the quarantine flag
and it runs:

```bash
tar -xzf pd-status-wall-macos-intel.tar.gz
xattr -d com.apple.quarantine pd-status-wall
./pd-status-wall
```

Or, after the first blocked attempt, go to **System Settings → Privacy &
Security** and click **Open Anyway**.

### macOS — Apple Silicon (M1/M2/M3/M4)

Identical to Intel apart from the filename. Homebrew picks the right
architecture for you, so there is nothing to get wrong:

```bash
brew install nicolasb114/tap/pd-status-wall
pd-status-wall
```

Without Homebrew:

```bash
curl -L https://github.com/nicolasb114/pd-status-wall/releases/latest/download/pd-status-wall-macos-apple-silicon.tar.gz | tar xz
./pd-status-wall
```

A browser download hits the same Gatekeeper block described under Intel
above, with the same two fixes.

**Which Mac do I have?**  → About This Mac. "Chip: Apple M…" means Apple
Silicon; "Processor: Intel…" means Intel. From a terminal, `uname -m`
prints `arm64` or `x86_64`.

### Day to day

The macOS and Linux downloads include a launcher next to the binary —
**`Start Status Wall.command`** on macOS, **`start.sh`** on Linux.
Double-click it (macOS) or run it (Linux) and the app starts with its
settings pinned to that folder.

That pinning is the point. By default the app keeps its configuration in
`./data` relative to wherever it was launched from, and a Finder
double-click runs from your home folder — so launching the binary
directly finds no settings and generates a new admin password every time.
The launcher removes that trap, and moving the folder moves your whole
setup with it.

Windows needs no launcher: Explorer already runs an `.exe` from its own
folder, so `data` lands beside it. Keep the `.exe` somewhere permanent
rather than in Downloads.

## First run

On first start the app generates an admin password, prints it once, and
writes its configuration to `./data` next to wherever you launched it:

```
==============================================================
 Admin password generated. Save it now - it will not be shown again.
 Username: admin
 Password: aB3xk9Qz...
==============================================================
```

The username is fixed at `admin`. The password is yours to change in the
admin panel. If it's ever lost, start the app once with
`RESET_ADMIN_PASSWORD=true` and it will print a new one.

Two things worth knowing immediately:

- **Configuration follows your working directory.** `./data` is relative,
  so launching from a different folder gives you a *fresh* install with a
  *new* password rather than your existing setup. Set `DATA_DIR` to a
  fixed path to avoid that.
- **`data/config.json` holds the API key and the password hash**, and the
  app creates it with `0600` permissions. Treat it as a secret.

## Using it

Everything is configured at `/admin`. The display page itself needs no
login and has no controls — only a small, faded gear icon in the
bottom-right corner that leads to the admin panel.

### PagerDuty connection

Paste a read-only API key, choose your region (`US` or `EU` — ask whoever
administers the account if you're unsure), and optionally change the poll
interval. The default is 45 seconds; the minimum is 10. PagerDuty's rate
limits are respected automatically: on a `429` the poller waits until the
limit resets rather than retrying straight away.

### Services and groups

Choose which status page to mirror, then order the business services it
shows. That order also positions groups, so there's nothing separate to
arrange.

**Groups** reproduce the grouping you set up in PagerDuty's status page
editor, which the API doesn't expose. Name a group, tick the services that
belong to it, and it appears at the position of its first member, showing
the worst status among them. Services left ungrouped appear on their own.

**Supporting services** is off by default, matching PagerDuty's own status
page. Turning it on adds one expandable level of the services directly
supporting each business service — useful on a NOC wall for seeing which
underlying component broke. It deliberately stops at one level: PagerDuty's
dependency graph is an internal impact-calculation model, and following it
to its full depth produces a deep, heavily duplicated tree that looks
nothing like a status page.

### Branding and theme

| Setting | What it does |
|---|---|
| Logo, banner image | Uploaded and stored locally; banner fit is `contain` (whole image, may letterbox) or `cover` (fills, may crop) |
| Light or dark | `Light`, `Dark`, or follow the screen's own OS setting |
| Display mode | Full colour, or black & white |
| Columns | Fit automatically, or pin 1–4 |
| Primary / accent colour | Buttons, borders and accents |
| Text colour | Headline and body text |
| Status banner | Alignment (left/centre), size (small → extra large), font |

Two deliberate behaviours:

- **Black & white mode doesn't rely on colour alone.** Each status also
  has a distinct shape (circle, square, triangle…) and a text label, so it
  stays readable on a monochrome projector and for colour-blind viewers.
- **An unreadable text colour is overridden.** A colour chosen against a
  light background becomes invisible on a dark one. Since this is an
  unattended display, the app checks the colour against the active
  background and falls back to a readable default below WCAG AA (4.5:1),
  telling you in the admin panel rather than silently ignoring the choice.

Pinned column counts still collapse on narrow screens — the count is a
preference for the big display, not a promise to honour at phone width.

### Buttons

Label-and-URL pairs shown as buttons, for things like "Report an issue" or
an internal runbook.

The row has a **position** (in the banner, between the banner and the
status headline, above the services, below them, or a fixed footer bar),
an **alignment**, a **style** and a **size**. Individual buttons can
override the style and carry their own colour; left blank they inherit the
row, so one stand-out button doesn't mean configuring all of them.

### Security and network

- **Change the password** from the admin panel (current password required).
- **IP allowlist** restricts access to given CIDR ranges — defence in
  depth, not a substitute for the network controls below.
- **Poller status** shows the last successful poll and any error, so you
  can tell the data is stale even while the display keeps showing the last
  good state.

## Network and security

The app is built to run inside a private network with **no inbound exposure
to the internet**. It has no webhook receiver by design; all PagerDuty
communication is outbound polling.

Because the app can't see the network it's deployed on, true
"internal-only" has to be enforced at the infrastructure layer:

- **Bind to a specific internal interface** rather than every interface:
  `LISTEN_ADDR=10.0.5.20:8080`.
- **Firewall the port** so only the intended subnet or VLAN can reach it,
  and no internet-facing interface can.
- **Never port-forward or NAT this port** on a perimeter firewall.
- Keep the host on an internal-only network segment.

As an extra layer — not a replacement — the **IP allowlist** in the admin
panel rejects requests from outside the CIDR ranges you configure. Leaving
it empty allows everything, which only makes sense when the network layer
is already doing this job.

### TLS

Plain HTTP is the default, which is usually fine on an isolated internal
segment. If your policy requires encryption internally, set
`TLS_CERT_FILE` and `TLS_KEY_FILE` to serve HTTPS — otherwise the admin
password crosses the local network in clear text.

### Admin access

A single fixed username (`admin`) with a bcrypt-hashed password.
Deliberately simple — no SSO, no per-user accounts — proportionate to a
read-only API key on an internal-only page. Sessions are server-side,
cookie-based, and expire after 12 hours.

## Configuration reference

Most settings live in the admin panel. These are read once at startup,
because they determine how the process binds to the network:

| Variable | Default | Purpose |
|---|---|---|
| `DATA_DIR` | `./data` | Where `config.json` and uploaded images live |
| `LISTEN_ADDR` | `0.0.0.0:8080` | Address and port to bind |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | unset | Set both to serve HTTPS |
| `RESET_ADMIN_PASSWORD` | unset | `true` generates and prints a new admin password at startup |

```bash
DATA_DIR=/var/lib/pd-status-wall LISTEN_ADDR=10.0.5.20:8080 ./pd-status-wall
```

The binary also answers `--version` and `--help`.

## Deployment

Copy the binary to the target machine and run it — no database, no reverse
proxy, no runtime dependencies. A minimal `systemd` unit:

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

Point the display at `http://<host>:<port>/` and leave it. The page polls
for updates on its own and recovers from failures without intervention.

## How it works

One process, two parts:

1. A **background poller** calls the PagerDuty REST API on a timer and
   refreshes an in-memory snapshot of current status.
2. An **HTTP server** serves three things, with all static assets embedded
   in the binary:
   - `/` — the display page, no login
   - `/api/state` — JSON the display polls every ~30s to update in place,
     without a reload or a flicker
   - `/admin` — the settings panel, behind the login

Status comes from PagerDuty's own computed impact state rather than being
recalculated here: the account's priority threshold is a global PagerDuty
setting, so asking PagerDuty whether a business service is impacted is
both simpler and correct by definition.

Worst case, a change in PagerDuty reaches the display in about 75 seconds
(up to 45s for the next poll, plus up to 30s for the browser's next
fetch); typically it's around 35.

## Troubleshooting

**The display says "Not configured yet."**
No API key or status page is set. Finish the setup in `/admin`.

**The admin panel reports a poller error, but the display looks fine.**
Working as intended — the display keeps the last known-good state while
the poller is failing. The error is under **Security & Network → Poller
status**.

**Every page returns 403.**
An IP allowlist is configured and your address isn't in it. Add your range
or clear the list.

**My settings vanished and it printed a new password.**
The app was launched from a different directory, so it created a fresh
`./data`. Set `DATA_DIR` to a fixed path.

**I lost the admin password.**
Start it once with `RESET_ADMIN_PASSWORD=true ./pd-status-wall`.

**macOS killed it with no message.**
Gatekeeper, because the build isn't notarized. See the macOS install
sections above.

## Building from source

Requires Go 1.26+.

```bash
git clone https://github.com/nicolasb114/pd-status-wall.git
cd pd-status-wall
go build -o pd-status-wall .
./pd-status-wall
```

Cross-compiling:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o pd-status-wall-linux-x86-64 .
```

`GOOS`/`GOARCH` are Go's own names: `darwin` means macOS, and `amd64`
means 64-bit Intel/AMD rather than AMD alone. Release assets are named
after the machine instead, so nobody has to translate.

## License

[MIT](LICENSE)
