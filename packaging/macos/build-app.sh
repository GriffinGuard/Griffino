#!/usr/bin/env bash
#
# build-app.sh — assemble Griffino.app from a built GUI griffino binary.
#
# Usage:
#   packaging/macos/build-app.sh <gui-binary> <version> [output-dir]
#
#   <gui-binary>  Path to a griffino binary built with `-tags gui` (CGO + Cocoa,
#                 so it must be built on macOS). This binary provides the `tray`
#                 subcommand that shows the menu-bar icon.
#   <version>     Version string substituted into Info.plist (e.g. 0.1.0).
#   [output-dir]  Where to write Griffino.app (default: ./dist/macos).
#
# Bundle layout produced:
#   Griffino.app/
#     Contents/
#       Info.plist                 (CFBundleExecutable=Griffino, LSUIElement=1)
#       MacOS/
#         Griffino                 (launcher script: exec ./griffino-bin tray)
#         griffino-bin             (the real GUI binary)
#       Resources/
#         Griffino.icns
#
# macOS cannot pass arguments to CFBundleExecutable via Info.plist, so the
# bundle executable is a tiny launcher script that execs the real griffino
# binary with `tray`. Info.plist sets LSUIElement so the app runs as a
# menu-bar agent (no Dock icon).
#
# The real binary is named `griffino-bin` (not `griffino`) so it cannot collide
# with the `Griffino` launcher on case-insensitive macOS filesystems (the
# default), where `Griffino` and `griffino` would be the same path.
set -euo pipefail

usage() {
	echo "usage: $0 <gui-binary> <version> [output-dir]" >&2
	exit 2
}

[ "$#" -ge 2 ] || usage

BIN="$1"
VERSION="$2"
OUT_DIR="${3:-dist/macos}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLIST_TEMPLATE="$SCRIPT_DIR/Info.plist"
ICNS="$SCRIPT_DIR/Griffino.icns"

[ -f "$BIN" ]            || { echo "error: gui binary not found: $BIN" >&2; exit 1; }
[ -f "$PLIST_TEMPLATE" ] || { echo "error: missing Info.plist template: $PLIST_TEMPLATE" >&2; exit 1; }
[ -f "$ICNS" ]           || { echo "error: missing icon: $ICNS" >&2; exit 1; }

APP="$OUT_DIR/Griffino.app"
CONTENTS="$APP/Contents"

echo ">> building $APP (version $VERSION)"
rm -rf "$APP"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"

# Real GUI binary (named griffino-bin to avoid a case-insensitive collision
# with the Griffino launcher).
cp "$BIN" "$CONTENTS/MacOS/griffino-bin"
chmod 755 "$CONTENTS/MacOS/griffino-bin"

# Launcher script set as CFBundleExecutable: execs the real binary with `tray`.
cat > "$CONTENTS/MacOS/Griffino" <<'LAUNCHER'
#!/bin/sh
# Griffino.app launcher: run the menu-bar tray over the headless daemon.
DIR="$(cd "$(dirname "$0")" && pwd)"
exec "$DIR/griffino-bin" tray "$@"
LAUNCHER
chmod 755 "$CONTENTS/MacOS/Griffino"

# Icon.
cp "$ICNS" "$CONTENTS/Resources/Griffino.icns"

# Info.plist with the version substituted.
sed "s/__VERSION__/${VERSION}/g" "$PLIST_TEMPLATE" > "$CONTENTS/Info.plist"

# Validate the bundle's plist.
plutil -lint "$CONTENTS/Info.plist" >/dev/null

echo ">> done: $APP"
echo "   executable : $CONTENTS/MacOS/Griffino -> griffino-bin tray"
echo "   icon       : $CONTENTS/Resources/Griffino.icns"
echo "   note       : unsigned by current release policy"
echo "                (see docs/gui-macos-packaging.md)."
