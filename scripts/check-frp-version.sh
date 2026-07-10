#!/usr/bin/env bash
# Assert the FRP_VERSION pin agrees across every place it is declared in this
# repo. frpc must be built/downloaded at one version; a drift silently ships a
# client that can mismatch the frps server. REtunnel (Dockerfile, Justfile)
# holds the server pin and MUST be kept equal by hand — this check cannot see
# another repo.
set -euo pipefail

# The recipe file is tracked as "Justfile"; resolve case-robustly so this works
# on both case-sensitive (Linux CI) and case-insensitive (macOS) filesystems.
if [ -f Justfile ]; then
    justfile_path="Justfile"
elif [ -f justfile ]; then
    justfile_path="justfile"
else
    echo "ERROR: no Justfile/justfile found" >&2
    exit 1
fi

embedded=$(grep -oE 'FRP_VERSION = "[^"]+"' src/embedded/frpc.go | cut -d'"' -f2)
buildsh=$(grep -oE 'FRP_VERSION="[^"]+"' scripts/build.sh | head -1 | cut -d'"' -f2)
justf=$(grep -oE 'FRP_VERSION := "[^"]+"' "$justfile_path" | cut -d'"' -f2)

echo "src/embedded/frpc.go: $embedded"
echo "scripts/build.sh:     $buildsh"
echo "$justfile_path:             $justf"

if [ "$embedded" != "$buildsh" ] || [ "$embedded" != "$justf" ]; then
    echo "ERROR: FRP_VERSION pins disagree across the repo" >&2
    exit 1
fi

echo "OK: FRP_VERSION is $embedded everywhere in this repo"
echo "REMINDER: REtunnel Dockerfile + Justfile must also be v$embedded"
