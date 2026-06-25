#!/usr/bin/env bash
#
# Build the Windows MSI from a compiled griffino.exe using the WiX Toolset v5
# (`wix build`, a cross-platform .NET tool). WiX—unlike the old wixl/msitools—
# supports <Environment> (PATH) and <Shortcut>, which the MSI now uses.
#
# Usage:
#   scripts/build-msi.sh <version> <path-to-griffino.exe> <output.msi> [arch]
#
# <version> may carry a leading v and/or a prerelease suffix (e.g. v1.0.0-rc.1);
# the MSI ProductVersion is the numeric X.Y.Z part only, as MSI requires.
#
set -euo pipefail

VERSION="${1:?version required}"
EXE="${2:?path to griffino.exe required}"
OUT="${3:?output .msi path required}"
ARCH="${4:-x64}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WXS="${REPO_ROOT}/packaging/windows/griffino.wxs"

[ -f "$EXE" ] || { echo "error: exe not found: $EXE" >&2; exit 1; }
command -v wix >/dev/null 2>&1 || {
  echo "error: wix (WiX Toolset v5) not found; install with: dotnet tool install --global wix" >&2
  exit 1
}

# MSI ProductVersion must be numeric X.Y.Z[.B]: strip leading v and any -suffix.
NUM="${VERSION#v}"
MSIVER="${NUM%%-*}"

# wix is a native Windows tool; under Git Bash hand it Windows-form paths
# (cygpath -m keeps forward slashes, which .NET accepts) for the file arguments.
WXS_ARG="$WXS"; EXE_ARG="$EXE"; OUT_ARG="$OUT"
if command -v cygpath >/dev/null 2>&1; then
  WXS_ARG="$(cygpath -m "$WXS")"
  EXE_ARG="$(cygpath -m "$EXE")"
  OUT_ARG="$(cygpath -m "$OUT")"
fi

wix build -arch "$ARCH" -d "Version=$MSIVER" -d "BinPath=$EXE_ARG" -o "$OUT_ARG" "$WXS_ARG"
echo "Built $OUT (ProductVersion=$MSIVER, arch=$ARCH)"
