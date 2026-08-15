#!/usr/bin/env sh
# Installs the go-dicom CLI from a GitHub release.
#
#   ./install.sh                      the latest release
#   ./install.sh v1.5.0               a specific one
#   PREFIX=/usr/local/bin ./install.sh
#
# Or in one step, having read it first:
#
#   curl -fsSL https://raw.githubusercontent.com/amrshadid/go-dicom/main/install.sh | sh
#
# What it does: works out which binary suits this machine, downloads it with the
# release's SHA256SUMS, refuses to install unless the checksum matches, and puts it
# somewhere on PATH.
#
# POSIX sh rather than bash, so it runs on a minimal container as well as on a Mac.

set -eu

REPO="amrshadid/go-dicom"
BIN="dicom"
VERSION="${1:-latest}"

say()  { printf '%s\n' "$*"; }
die()  { printf 'install: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed."; }
need curl

# --- which binary --------------------------------------------------------------
os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Darwin) os_name=macos ;;
  Linux)  os_name=linux ;;
  # Windows is built and published, but this script cannot sensibly install it:
  # there is no agreed place to put it and no PATH convention to rely on.
  MINGW*|MSYS*|CYGWIN*)
    die "on Windows, download dicom-windows-amd64.exe from
       https://github.com/$REPO/releases/latest
     and put it in a directory on your PATH." ;;
  *) die "unsupported operating system: $os" ;;
esac

case "$arch" in
  arm64|aarch64) arch_name=arm64 ;;
  x86_64|amd64)  arch_name=amd64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

asset="${BIN}-${os_name}-${arch_name}"

# The published set. A combination outside it should say so rather than 404 later.
case "$asset" in
  dicom-macos-arm64|dicom-macos-amd64|dicom-linux-amd64|dicom-linux-arm64) ;;
  *) die "no published binary for ${os_name}/${arch_name}. Build from source:
       go build -o dicom github.com/$REPO" ;;
esac

# --- where to put it -----------------------------------------------------------
# A directory already on PATH and writable, so nothing needs sudo and nothing needs
# a shell profile edited. /usr/local/bin is the fallback, and that one may.
if [ -n "${PREFIX:-}" ]; then
  target="$PREFIX"
else
  target=""
  for d in "$HOME/.local/bin" "$HOME/bin"; do
    case ":$PATH:" in *":$d:"*) [ -d "$d" ] && [ -w "$d" ] && { target="$d"; break; } ;; esac
  done
  [ -n "$target" ] || target=/usr/local/bin
fi

# --- download ------------------------------------------------------------------
if [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

tmp="$(mktemp -d)"
# shellcheck disable=SC2064
trap "rm -rf '$tmp'" EXIT INT TERM

say "downloading $asset ($VERSION)…"
curl -fsSL -o "$tmp/$asset" "$base/$asset" \
  || die "could not download $base/$asset"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" \
  || die "could not download the checksums; refusing to install unverified."

# --- verify --------------------------------------------------------------------
# Before running it, not after. A binary is the one kind of download where
# "probably fine" is not good enough.
if command -v shasum >/dev/null 2>&1; then
  got="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else
  die "neither shasum nor sha256sum is available, so the download cannot be verified."
fi

want="$(awk -v a="$asset" '$2 == a || $2 == "*"a {print $1}' "$tmp/SHA256SUMS")"
[ -n "$want" ] || die "$asset is not listed in SHA256SUMS."
[ "$want" = "$got" ] || die "checksum mismatch for $asset.
     expected $want
     got      $got"

say "checksum ok"

# --- install -------------------------------------------------------------------
chmod +x "$tmp/$asset"

# A browser download carries a quarantine flag and Gatekeeper refuses to run it.
# curl does not set one, but clearing it is harmless and covers a re-run over a file
# that was fetched some other way.
if [ "$os_name" = macos ] && command -v xattr >/dev/null 2>&1; then
  xattr -d com.apple.quarantine "$tmp/$asset" 2>/dev/null || true
fi

if [ -w "$target" ]; then
  install -m 755 "$tmp/$asset" "$target/$BIN"
else
  say "$target needs elevated permission:"
  sudo install -m 755 "$tmp/$asset" "$target/$BIN"
fi

say ""
say "installed $target/$BIN"
"$target/$BIN" version || true

case ":$PATH:" in
  *":$target:"*) ;;
  *) say ""
     say "$target is not on your PATH. Add it:"
     say "    echo 'export PATH=\"$target:\$PATH\"' >> ~/.zshrc && exec zsh" ;;
esac

say ""
say "try:  $BIN help"
say "remove with:  rm $target/$BIN"
