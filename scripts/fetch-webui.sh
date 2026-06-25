#!/usr/bin/env bash
#
# Fetch the prebuilt Griffino Web UI and stage it for go:embed.
#
# Reads the pinned version from internal/api/web/UI_VERSION, downloads the
# matching tarball + checksum from the public Griffino-WebUI releases, verifies
# it, and extracts into internal/api/web/dist/. A value of "placeholder" (or an
# empty file) leaves the committed placeholder in place — useful for local
# builds before the first UI release exists.
#
# Usage:
#   ./scripts/fetch-webui.sh              # fetch the version pinned in UI_VERSION
#   WEBUI_REPO=owner/repo ./scripts/fetch-webui.sh
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

WEBUI_REPO="${WEBUI_REPO:-GriffinGuard/Griffino-WebUI}"
DIST_DIR="internal/api/web/dist"
VERSION_FILE="internal/api/web/UI_VERSION"

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarn:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[ -f "$VERSION_FILE" ] || die "missing ${VERSION_FILE}"
VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"

if [ -z "$VERSION" ] || [ "$VERSION" = "placeholder" ]; then
    warn "UI_VERSION is '${VERSION:-empty}' — keeping the committed placeholder, not fetching."
    exit 0
fi

NUM="${VERSION#v}"
ASSET="griffino-webui_${NUM}.tar.gz"
BASE="https://github.com/${WEBUI_REPO}/releases/download/${VERSION}"

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
info "Fetching Web UI ${VERSION} from ${WEBUI_REPO}"
curl -fsSL "${BASE}/${ASSET}" -o "${TMP}/${ASSET}" || die "download failed: ${ASSET}"

if curl -fsSL "${BASE}/checksums.txt" -o "${TMP}/checksums.txt" 2>/dev/null; then
    ( cd "$TMP" && grep " ${ASSET}\$" checksums.txt | shasum -a 256 -c - ) \
        || die "checksum verification failed"
    info "checksum OK"
else
    warn "checksums.txt not found; skipping verification"
fi

info "Staging into ${DIST_DIR}"
rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"
tar -xzf "${TMP}/${ASSET}" -C "${DIST_DIR}"
[ -f "${DIST_DIR}/index.html" ] || die "extracted archive has no index.html at its root"
info "Web UI ${VERSION} staged into ${DIST_DIR}."
