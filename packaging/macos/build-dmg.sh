#!/usr/bin/env bash
#
# build-dmg.sh — wrap Griffino.app into a distributable .dmg.
#
# Usage:
#   packaging/macos/build-dmg.sh <Griffino.app> <version> [output-dir]
#
#   <Griffino.app>  Path to the .app produced by build-app.sh.
#   <version>       Version string used in the dmg name (e.g. 0.1.0).
#   [output-dir]    Where to write the dmg (default: ./dist/macos).
#
# Produces Griffino-<version>.dmg: a read-only compressed image containing
# Griffino.app plus an /Applications symlink so the user can drag-to-install.
set -euo pipefail

usage() {
	echo "usage: $0 <Griffino.app> <version> [output-dir]" >&2
	exit 2
}

[ "$#" -ge 2 ] || usage

APP="$1"
VERSION="$2"
OUT_DIR="${3:-dist/macos}"

[ -d "$APP" ] || { echo "error: app bundle not found: $APP" >&2; exit 1; }

mkdir -p "$OUT_DIR"
DMG="$OUT_DIR/Griffino-${VERSION}.dmg"

# Stage a directory with the .app and an Applications symlink.
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

echo ">> building $DMG"
rm -f "$DMG"
hdiutil create \
	-volname "Griffino" \
	-srcfolder "$STAGE" \
	-fs HFS+ \
	-format UDZO \
	-ov \
	"$DMG" >/dev/null

echo ">> done: $DMG"
echo "   note: unsigned/un-notarized by current release policy. On first launch users right-click the"
echo "         app and choose Open to bypass Gatekeeper (see"
echo "         docs/gui-macos-packaging.md)."
