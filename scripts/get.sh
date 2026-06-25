#!/usr/bin/env bash
#
# Download and install a prebuilt Griffino release binary for your OS/arch.
#
#   curl -fsSL https://raw.githubusercontent.com/GriffinGuard/Griffino/main/scripts/get.sh | bash
#   VERSION=v1.0.0 ./scripts/get.sh
#   PREFIX=~/.local/bin ./scripts/get.sh
#
# For building from source instead, use scripts/install.sh.
#
set -euo pipefail

REPO="GriffinGuard/Griffino"
BIN_NAME="griffino"
VERSION="${VERSION:-latest}"
PREFIX="${PREFIX:-}"

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarn:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# ── detect os/arch ────────────────────────────────────────────────────────────
OS="$(uname -s)"; ARCH="$(uname -m)"
case "$OS" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *) die "unsupported OS: $OS (use scripts/install.sh to build from source)" ;;
esac
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) die "unsupported arch: $ARCH" ;;
esac

# ── resolve version ───────────────────────────────────────────────────────────
if [ "$VERSION" = "latest" ]; then
    VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
    [ -n "$VERSION" ] || die "could not resolve latest release tag"
fi
NUM="${VERSION#v}"
info "Installing ${BIN_NAME} ${VERSION} (${OS}/${ARCH})"

# ── download + verify + extract ───────────────────────────────────────────────
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
ASSET="${BIN_NAME}_${NUM}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${REPO}/releases/download/${VERSION}"

curl -fsSL "${BASE}/${ASSET}" -o "${TMP}/${ASSET}" || die "download failed: ${ASSET}"
if curl -fsSL "${BASE}/checksums.txt" -o "${TMP}/checksums.txt" 2>/dev/null; then
    ( cd "$TMP" && grep " ${ASSET}\$" checksums.txt | shasum -a 256 -c - ) \
        || die "checksum verification failed"
    info "checksum OK"
else
    warn "checksums.txt not found; skipping verification"
fi
tar -xzf "${TMP}/${ASSET}" -C "$TMP"

# ── install ───────────────────────────────────────────────────────────────────
if [ -z "$PREFIX" ]; then
    if [ -w /usr/local/bin ] 2>/dev/null; then PREFIX="/usr/local/bin"; else PREFIX="$HOME/.local/bin"; fi
fi
mkdir -p "$PREFIX"
DEST="${PREFIX%/}/${BIN_NAME}"
if [ -w "$PREFIX" ]; then install -m 0755 "${TMP}/${BIN_NAME}" "$DEST"; else sudo install -m 0755 "${TMP}/${BIN_NAME}" "$DEST"; fi
info "Installed ${DEST}"

case ":${PATH}:" in *":${PREFIX%/}:"*) ;; *) warn "add ${PREFIX%/} to PATH: export PATH=\"${PREFIX%/}:\$PATH\"" ;; esac

command -v docker >/dev/null 2>&1 || warn "Docker not found — required at runtime for 'griffino daemon'."
"$DEST" version
