#!/usr/bin/env bash
#
# Build Griffino from source and install the binary onto your PATH.
#
# Usage:
#   ./scripts/install.sh                # build + install to a sensible prefix
#   PREFIX=/usr/local/bin ./scripts/install.sh
#   ./scripts/install.sh --prefix ~/.local/bin
#   ./scripts/install.sh --build-only   # just produce ./griffino, don't install
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

BIN_NAME="griffino"
CMD_PKG="./cmd/griffino"
MIN_GO="1.25"
PREFIX="${PREFIX:-}"
BUILD_ONLY=0

# ── args ─────────────────────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
    case "$1" in
        --prefix) PREFIX="$2"; shift 2 ;;
        --prefix=*) PREFIX="${1#*=}"; shift ;;
        --build-only) BUILD_ONLY=1; shift ;;
        -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarn:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# ── 1. toolchain ─────────────────────────────────────────────────────────────
command -v go >/dev/null 2>&1 || die "Go toolchain not found. Install Go >= ${MIN_GO} (https://go.dev/dl/)."
GO_VER="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
# numeric compare major.minor
if [ "$(printf '%s\n%s\n' "$MIN_GO" "$GO_VER" | sort -V | head -1)" != "$MIN_GO" ]; then
    die "Go ${MIN_GO}+ required, found ${GO_VER}."
fi
info "Using Go ${GO_VER}"

# ── 2. build ─────────────────────────────────────────────────────────────────
# Stage the embedded Web UI (no-op while UI_VERSION is "placeholder").
info "Staging Web UI"
bash "${REPO_ROOT}/scripts/fetch-webui.sh" || warn "Web UI fetch failed; building with the placeholder console."

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
info "Building ${BIN_NAME} (${VERSION})"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.date=${BUILD_DATE}" -o "${BIN_NAME}" "${CMD_PKG}"
info "Built ./${BIN_NAME}"

if [ "$BUILD_ONLY" -eq 1 ]; then
    info "Build-only: leaving binary at ${REPO_ROOT}/${BIN_NAME}"
    exit 0
fi

# ── 3. choose install prefix ─────────────────────────────────────────────────
if [ -z "$PREFIX" ]; then
    if [ -w /usr/local/bin ] 2>/dev/null; then
        PREFIX="/usr/local/bin"
    else
        PREFIX="$HOME/.local/bin"
    fi
fi
mkdir -p "$PREFIX"

# ── 4. install (sudo only if needed) ─────────────────────────────────────────
DEST="${PREFIX%/}/${BIN_NAME}"
if [ -w "$PREFIX" ]; then
    install -m 0755 "${BIN_NAME}" "$DEST"
else
    warn "${PREFIX} is not writable; using sudo"
    sudo install -m 0755 "${BIN_NAME}" "$DEST"
fi
info "Installed ${DEST}"

case ":${PATH}:" in
    *":${PREFIX%/}:"*) ;;
    *) warn "${PREFIX} is not on your PATH. Add to your shell profile:"; echo "    export PATH=\"${PREFIX%/}:\$PATH\"" ;;
esac

# ── 5. runtime dependency check (Docker) ─────────────────────────────────────
if command -v docker >/dev/null 2>&1; then
    if docker info >/dev/null 2>&1; then
        info "Docker is running."
    else
        warn "Docker is installed but not running. Start Docker Desktop before 'griffino daemon'."
    fi
else
    warn "Docker not found. Griffino's daemon orchestrates RabbitMQ/Redis as containers and needs Docker."
    warn "Install Docker Desktop: https://www.docker.com/products/docker-desktop/"
fi

cat <<EOF

Done. Next steps:
  griffino daemon            # start the daemon (requires Docker running)
  griffino dev install <dir> # install a local plugin
  griffino --help

Config (optional): ~/.griffino/config.yaml   Data: ~/.griffino/
EOF
