#!/bin/sh
# Runs as root during apt/dnf install. Griffino's service is per-user
# (systemctl --user), which root cannot enable for a specific user here, so we
# only print guidance — the user runs `griffino service install` themselves.
set -e

cat <<'EOF'

Griffino installed.

Next steps:
  1. Ensure Docker (or a compatible container runtime) is installed and running.
  2. Start Griffino on login as a per-user service:
       griffino service install
       griffino service start
  3. Open the web console:  http://localhost:7070

Docs: https://github.com/GriffinGuard/Griffino
EOF

exit 0
