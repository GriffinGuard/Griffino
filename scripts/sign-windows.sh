#!/usr/bin/env bash
#
# Pluggable Windows code-signing seam for self-distributed artifacts (.msi/.msix).
#
# Why this exists: the Microsoft Store signs the MSIX it ingests, so the Store
# channel needs no signing here. But the directly-downloadable MSI/MSIX still
# trip SmartScreen while unsigned. We can't use SignPath Foundation yet (it
# requires established project reputation) and Azure Trusted Signing gates
# individual publishers — so for now signing is a NO-OP. This script isolates
# that decision behind one contract so a provider can be dropped in later by
# editing ONLY this file + adding a CI secret, with no change to packaging.
#
# Contract:
#   scripts/sign-windows.sh <artifact-path>
#   - Selects a provider from $WINDOWS_SIGNING_PROVIDER (default: none).
#   - "none": pass-through, exit 0, artifact left unsigned.
#   - Any real provider must sign <artifact-path> IN PLACE and exit non-zero on
#     failure.
#
# To enable SignPath later (sketch): set WINDOWS_SIGNING_PROVIDER=signpath and
# implement the case below — typically submit <artifact> to SignPath and replace
# it with the signed result. Azure Trusted Signing would be a signtool.exe call
# with the trusted-signing dlib. Keep all provider specifics inside this file.
#
set -euo pipefail

ARTIFACT="${1:?artifact path required}"
PROVIDER="${WINDOWS_SIGNING_PROVIDER:-none}"

[ -f "$ARTIFACT" ] || { echo "error: artifact not found: $ARTIFACT" >&2; exit 1; }

case "$PROVIDER" in
  none)
    echo "sign-windows: provider=none — leaving $ARTIFACT unsigned (Store signs on ingestion)."
    ;;
  signpath)
    echo "error: WINDOWS_SIGNING_PROVIDER=signpath is not implemented yet." >&2
    echo "       Wire SignPath here once the project qualifies for the Foundation program." >&2
    exit 1
    ;;
  azure-trusted-signing)
    echo "error: WINDOWS_SIGNING_PROVIDER=azure-trusted-signing is not implemented yet." >&2
    echo "       Wire signtool + the Trusted Signing dlib here when a cert is available." >&2
    exit 1
    ;;
  *)
    echo "error: unknown WINDOWS_SIGNING_PROVIDER='$PROVIDER'" >&2
    exit 1
    ;;
esac
