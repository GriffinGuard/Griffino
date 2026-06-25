#!/usr/bin/env bash
#
# Build the unsigned Windows MSIX from a compiled griffino.exe + the
# griffino-startup.exe launcher, using makeappx (Windows SDK). Intended to run
# on a windows-latest CI runner (git-bash); makeappx is Windows-only, so this is
# additive to — and independent of — the Linux MSI path (scripts/build-msi.sh).
#
# The Store signs the package on ingestion, so the output here is intentionally
# unsigned. For a self-distributed signed package, pipe the result through
# scripts/sign-windows.sh (a no-op until a signing provider is configured).
#
# Usage:
#   scripts/build-msix.sh <version> <griffino.exe> <griffino-startup.exe> <out.msix>
#
# Identity is supplied via environment from the Partner Center reservation (set
# as repo variables; see the release workflow). All three are required — the
# script errors if any is unset, rather than guessing an identity:
#   MSIX_IDENTITY_NAME           Package/Identity/Name        (e.g. <Account>.<App>)
#   MSIX_PUBLISHER               Package/Identity/Publisher   (e.g. CN=<GUID>)
#   MSIX_PUBLISHER_DISPLAY_NAME  Package/Properties/PublisherDisplayName
#
set -euo pipefail

VERSION="${1:?version required}"
EXE="${2:?path to griffino.exe required}"
STARTUP_EXE="${3:?path to griffino-startup.exe required}"
OUT="${4:?output .msix path required}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${REPO_ROOT}/packaging/windows/msix"

IDENTITY_NAME="${MSIX_IDENTITY_NAME:?MSIX_IDENTITY_NAME required (Partner Center Package/Identity/Name)}"
PUBLISHER="${MSIX_PUBLISHER:?MSIX_PUBLISHER required (Partner Center Package/Identity/Publisher, CN=...)}"
PUBLISHER_DISPLAY_NAME="${MSIX_PUBLISHER_DISPLAY_NAME:?MSIX_PUBLISHER_DISPLAY_NAME required (Partner Center PublisherDisplayName)}"

[ -f "$EXE" ] || { echo "error: exe not found: $EXE" >&2; exit 1; }
[ -f "$STARTUP_EXE" ] || { echo "error: startup exe not found: $STARTUP_EXE" >&2; exit 1; }
command -v makeappx >/dev/null 2>&1 || command -v makeappx.exe >/dev/null 2>&1 || {
  echo "error: makeappx (Windows SDK) not found on PATH" >&2; exit 1; }

# MSIX Version must be 4-part numeric X.Y.Z.B; strip a leading v and any -suffix,
# then append a .0 build field (Store revisions bump the tag, not this field).
NUM="${VERSION#v}"
SEMVER="${NUM%%-*}"
MSIXVER="${SEMVER}.0"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp "$EXE" "$STAGE/griffino.exe"
cp "$STARTUP_EXE" "$STAGE/griffino-startup.exe"
# Ship only the tile PNGs; the docs/generator alongside them stay out of the package.
mkdir -p "$STAGE/assets"
cp "$SRC"/assets/*.png "$STAGE/assets/"

# Substitute identity/version placeholders into the staged manifest.
sed -e "s|@IDENTITY_NAME@|${IDENTITY_NAME}|g" \
    -e "s|@PUBLISHER@|${PUBLISHER}|g" \
    -e "s|@PUBLISHER_DISPLAY_NAME@|${PUBLISHER_DISPLAY_NAME}|g" \
    -e "s|@VERSION@|${MSIXVER}|g" \
    "$SRC/AppxManifest.xml" > "$STAGE/AppxManifest.xml"

# makeappx is a native Windows tool taking /o /d /p switches. Under Git Bash
# (MSYS) on the CI runner, leading-slash args get rewritten into paths (e.g.
# "/o" -> "O:/"), so disable that conversion and hand it Windows-form paths.
STAGE_WIN="$(cygpath -w "$STAGE" 2>/dev/null || printf '%s' "$STAGE")"
OUT_WIN="$(cygpath -w "$OUT" 2>/dev/null || printf '%s' "$OUT")"
MSYS_NO_PATHCONV=1 makeappx pack /o /d "$STAGE_WIN" /p "$OUT_WIN"
echo "Built $OUT (Version=$MSIXVER, Identity=$IDENTITY_NAME, Publisher=$PUBLISHER)"
