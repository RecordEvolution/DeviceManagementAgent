#!/usr/bin/env bash
# Authenticode-sign Windows PE binaries from Linux CI using the IronFlock
# code-signing LEAF certificate, with a free RFC3161 timestamp so signatures
# stay valid after the leaf expires.
#
# The leaf PFX is provided base64-encoded in WINDOWS_SIGNING_PFX_B64 (a GitHub
# Actions secret), its password in WINDOWS_SIGNING_PFX_PASSWORD. When the
# secret is unset (pre-signing transition) this no-ops so releases still ship
# unsigned — the on-device verifier is non-fatal until the cutover.
#
# The leaf must chain to the offline root whose PUBLIC cert is embedded at
# src/codesign/roots/*.crt (see docs/WINDOWS-CODE-SIGNING.md), so
# on-device pinning accepts these signatures.
#
# Usage: scripts/sign-windows.sh <pe-file> [<pe-file> ...]
set -euo pipefail

if [ "${WINDOWS_SIGNING_PFX_B64:-}" = "" ]; then
    echo "WINDOWS_SIGNING_PFX_B64 is not set; skipping Authenticode signing (pre-cutover)."
    exit 0
fi

if ! command -v osslsigncode >/dev/null 2>&1; then
    sudo apt-get update
    sudo apt-get install -y osslsigncode
fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

pfx="$workdir/leaf.pfx"
echo "$WINDOWS_SIGNING_PFX_B64" | base64 -d > "$pfx"

# A couple of free public RFC3161 timestamp authorities; try in order.
TSAS=(
    "http://timestamp.digicert.com"
    "http://timestamp.sectigo.com"
)

sign_one() {
    local in="$1"
    local out="${in}.signed"
    local tsa
    for tsa in "${TSAS[@]}"; do
        if osslsigncode sign \
            -pkcs12 "$pfx" \
            -pass "${WINDOWS_SIGNING_PFX_PASSWORD:-}" \
            -n "IronFlock Device Agent" \
            -i "https://ironflock.com" \
            -h sha256 \
            -ts "$tsa" \
            -in "$in" -out "$out" 2>&1; then
            mv "$out" "$in"
            echo "signed $in (timestamp: $tsa)"
            return 0
        fi
        echo "timestamp $tsa failed, trying next..." >&2
    done
    echo "ERROR: could not sign $in with any timestamp authority" >&2
    return 1
}

for f in "$@"; do
    [ -f "$f" ] || { echo "skip missing $f"; continue; }
    sign_one "$f"
done
