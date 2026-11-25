#!/bin/sh
# Installs pd-status-wall on macOS or Linux.
#
# It works out which build your machine needs, checks the download against
# the checksums published with the release, and puts the binary on your
# PATH. Nothing is installed system-wide and nothing needs sudo by
# default.
#
#   curl -fsSL .../install.sh | sh          # convenient
#   curl -fsSL .../install.sh -o install.sh # cautious: read it, then run it
#   sh install.sh
#
# Environment:
#   PREFIX=/usr/local/bin   where to install   (default ~/.local/bin)
#   VERSION=v0.6.0          which release      (default: latest)

set -eu

REPO="nicolasb114/pd-status-wall"
PREFIX="${PREFIX:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"

die() { printf '\nError: %s\n' "$1" >&2; exit 1; }
say() { printf '%s\n' "$1"; }

# --- which build does this machine need -------------------------------

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Darwin)
    case "$arch" in
      arm64)          asset="pd-status-wall-macos-apple-silicon" ;;
      x86_64)         asset="pd-status-wall-macos-intel" ;;
      *) die "unsupported Mac architecture: $arch" ;;
    esac ;;
  Linux)
    case "$arch" in
      x86_64|amd64)   asset="pd-status-wall-linux-x86-64" ;;
      aarch64|arm64)  asset="pd-status-wall-linux-arm64" ;;
      *) die "unsupported Linux architecture: $arch" ;;
    esac ;;
  *)
    die "$os isn't supported by this script. On Windows, download
pd-status-wall-windows.exe from the releases page instead:
  https://github.com/$REPO/releases/latest" ;;
esac

archive="${asset}.tar.gz"
if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

command -v curl >/dev/null 2>&1 || die "curl is required but not installed."

# --- download ---------------------------------------------------------

tmp="$(mktemp -d)"
# shellcheck disable=SC2064  # expand $tmp now, not at trap time
trap "rm -rf '$tmp'" EXIT INT TERM

say "Installing pd-status-wall ($VERSION) for $os/$arch"
say "  downloading $archive"
curl -fsSL "$base/$archive" -o "$tmp/$archive" \
  || die "could not download $base/$archive"

# --- verify against the checksums published with the release ----------
#
# Skipped only if the release has no SHA256SUMS, which would mean an
# older release than the one this script was written for.

if curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" 2>/dev/null; then
  expected="$(grep " ${archive}\$" "$tmp/SHA256SUMS" | awk '{print $1}')"
  if [ -z "$expected" ]; then
    die "$archive is not listed in that release's SHA256SUMS."
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
  else
    actual=""
    say "  ! no sha256 tool found - skipping checksum verification"
  fi

  if [ -n "$actual" ]; then
    [ "$actual" = "$expected" ] || die "checksum mismatch for $archive.
  expected $expected
  got      $actual
Refusing to install. Try again, and if it persists, open an issue."
    say "  checksum verified"
  fi
else
  say "  ! this release publishes no SHA256SUMS - skipping verification"
fi

# --- install ----------------------------------------------------------

tar -xzf "$tmp/$archive" -C "$tmp" || die "could not unpack $archive"
[ -f "$tmp/pd-status-wall" ] || die "the archive didn't contain pd-status-wall."

mkdir -p "$PREFIX" || die "could not create $PREFIX"
install -m 0755 "$tmp/pd-status-wall" "$PREFIX/pd-status-wall" 2>/dev/null \
  || { cp "$tmp/pd-status-wall" "$PREFIX/pd-status-wall" && chmod 0755 "$PREFIX/pd-status-wall"; } \
  || die "could not write to $PREFIX. Choose another with PREFIX=/some/dir"

say ""
say "Installed to $PREFIX/pd-status-wall"
say "Version: $("$PREFIX/pd-status-wall" --version 2>/dev/null || echo unknown)"

# --- is it reachable --------------------------------------------------

case ":${PATH}:" in
  *":$PREFIX:"*)
    say ""
    say "Start it with:  pd-status-wall" ;;
  *)
    say ""
    say "$PREFIX is not on your PATH yet. Add it:"
    say ""
    say "  echo 'export PATH=\"$PREFIX:\$PATH\"' >> ~/.zshrc   # or ~/.bashrc"
    say "  exec \$SHELL"
    say ""
    say "Or run it directly:  $PREFIX/pd-status-wall" ;;
esac
