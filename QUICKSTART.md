<div align="center">

# pd-status-wall

**A single binary that turns your PagerDuty status page into a video wall display.**

[![CI](https://github.com/nicolasb114/pd-status-wall/actions/workflows/ci.yml/badge.svg)](https://github.com/nicolasb114/pd-status-wall/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/nicolasb114/pd-status-wall?label=latest%20release)](https://github.com/nicolasb114/pd-status-wall/releases/latest)

[**Download the latest release &rarr;**](https://github.com/nicolasb114/pd-status-wall/releases/latest)

</div>

---

## Install

1. Open the [latest release](https://github.com/nicolasb114/pd-status-wall/releases/latest).
2. Download the file matching your machine:

   | Platform | File |
   |---|---|
   | Linux (Intel/AMD) | `pd-status-wall-linux-amd64` |
   | Linux (ARM) | `pd-status-wall-linux-arm64` |
   | Mac (Apple Silicon) | `pd-status-wall-darwin-arm64` |
   | Mac (Intel) | `pd-status-wall-darwin-amd64` |
   | Windows | `pd-status-wall-windows-amd64.exe` |

3. Make it runnable and start it:

   ```bash
   chmod +x pd-status-wall-*
   ./pd-status-wall-*
   ```

That's it — no install step, no dependencies. The first time it runs, it prints a generated admin password to the terminal:

```
==============================================================
 Admin password generated. Save it now - it will not be shown again.
 Username: admin
 Password: aB3xk9Qz...
==============================================================
```

**Copy that password somewhere safe before it scrolls away.**

## Use it

| What | Where |
|---|---|
| The video wall display | `http://localhost:8080/` |
| Settings (admin panel) | `http://localhost:8080/admin` |

1. Open `http://localhost:8080/admin` and sign in with `admin` / the password from the terminal.
2. Paste a **read-only** PagerDuty API key ([how to create one](https://support.pagerduty.com/main/docs/api-access-keys)).
3. Pick your status page and the business services to show, in order.
4. Upload a logo/banner and pick your colors.
5. Open `http://localhost:8080/` on the video wall - it updates itself every ~30 seconds.

Need more detail (real deployment, HTTPS, restricting access, EU region, etc.)? See [README.md](README.md).
