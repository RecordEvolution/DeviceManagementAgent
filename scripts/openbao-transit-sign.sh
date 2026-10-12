# Runs INSIDE the OpenBao pod (POSIX sh), after scripts/openbao-admin.sh has
# set BAO_TOKEN; scripts/sign-release.sh sends it there. Kept in its own file
# so the integration test runs the same text against an OpenBao dev server.
#
# stdin:  one "<id> <base64 message>" line per signature
# stdout: one "<id> vault:v<N>:<base64 signature>" line per input line
# $1:     transit key name; $2: key version to sign with (empty: the latest)
#
# Stops at the first failure. The message reaches bao on stdin (input=-).
set -eu
key="$1"
version="${2:-}"
while read -r id input; do
    [ -n "$id" ] || continue
    if [ -n "$version" ]; then
        sig="$(printf '%s' "$input" | bao write -field=signature "transit/sign/$key" input=- key_version="$version")"
    else
        sig="$(printf '%s' "$input" | bao write -field=signature "transit/sign/$key" input=-)"
    fi
    printf '%s %s\n' "$id" "$sig"
done
