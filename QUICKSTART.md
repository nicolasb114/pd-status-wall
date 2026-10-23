<div align="center">

# pd-status-wall

**A single binary that turns your PagerDuty status page into a video wall display.**

[![CI](https://github.com/nicolasb114/pd-status-wall/actions/workflows/ci.yml/badge.svg)](https://github.com/nicolasb114/pd-status-wall/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/nicolasb114/pd-status-wall?label=latest%20release)](https://github.com/nicolasb114/pd-status-wall/releases/latest)

[**Download the latest release &rarr;**](https://github.com/nicolasb114/pd-status-wall/releases/latest)

</div>

---

## Install

### Mac and Linux - one command

```bash
brew install nicolasb114/tap/pd-status-wall
pd-status-wall
```

This is the smoothest route: Homebrew handles permissions and the macOS
security prompt for you, so there's nothing to click through.

No Homebrew? This works too, and avoids the same prompt:

```bash
curl -L https://github.com/nicolasb114/pd-status-wall/releases/latest/download/pd-status-wall-macos-apple-silicon.tar.gz | tar xz
./pd-status-wall
```

Swap the filename for your machine from the table below.

### Windows

Download [`pd-status-wall-windows.exe`](https://github.com/nicolasb114/pd-status-wall/releases/latest)
and double-click it.

Windows will likely say *"Windows protected your PC"* the first time -
that's SmartScreen flagging any app it hasn't seen before, not a sign
anything is wrong. Click **More info** &rarr; **Run anyway**.

### Downloading by hand

Grab the file for your machine from the
[latest release](https://github.com/nicolasb114/pd-status-wall/releases/latest):

| Your machine | File |
|---|---|
| Mac with an Apple chip (M1/M2/M3/M4) | `pd-status-wall-macos-apple-silicon.tar.gz` |
| Mac with an Intel chip | `pd-status-wall-macos-intel.tar.gz` |
| Linux (most servers and PCs) | `pd-status-wall-linux-x86-64.tar.gz` |
| Linux on ARM (Raspberry Pi, Graviton) | `pd-status-wall-linux-arm64.tar.gz` |
| Windows | `pd-status-wall-windows.exe` |

Not sure which Mac you have?  &rarr; About This Mac. "Chip: Apple M…"
means Apple Silicon; "Processor: Intel…" means Intel. (In a terminal,
`uname -m` prints `arm64` for Apple chips and `x86_64` for Intel.)

Unpack it and run:

```bash
tar -xzf pd-status-wall-*.tar.gz
./pd-status-wall
```

**On macOS, a file downloaded in a browser gets quarantined**, and since
these builds aren't notarized by Apple, macOS will refuse to run it
(it simply dies, with no useful message). Either install with Homebrew or
`curl` above - neither sets the quarantine flag - or clear it yourself:

```bash
xattr -d com.apple.quarantine pd-status-wall
```

You can also go to **System Settings &rarr; Privacy & Security** after the
first blocked attempt and click **Open Anyway**.

Verify a download against [`SHA256SUMS`](https://github.com/nicolasb114/pd-status-wall/releases/latest)
if you want: `shasum -a 256 pd-status-wall-*.tar.gz`.

---

The first time it runs, it prints a generated admin password to the terminal:

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
