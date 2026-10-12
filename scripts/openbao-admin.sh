#!/usr/bin/env bash
# Run a shell script inside the production OpenBao pod, logged in as role
# `admin` (the REDeployments secret-store pattern). The maintainer reaches
# OpenBao only through kubectl; nothing here needs a port-forward.
#
# The ServiceAccount JWT travels on stdin and the OpenBao token only in the
# pod's environment, so neither ever appears on a command line. Whatever the
# caller pipes into this script follows the JWT on the same stdin and is what
# the in-pod script reads.
#
# Usage: scripts/openbao-admin.sh '<script>' [args...]   (args are $1.. in the script)
# Env:   OPENBAO_KUBE_CONTEXT (default: gke_record-1283_europe-west1-b_ironflock-prod)
set -euo pipefail

context="${OPENBAO_KUBE_CONTEXT:-gke_record-1283_europe-west1-b_ironflock-prod}"
script="${1:?usage: openbao-admin.sh '<script>' [args...]}"
shift

# `jwt=-` makes bao read the JWT from its stdin (the printf pipe), not argv.
login='read -r JWT && BAO_TOKEN="$(printf %s "$JWT" | bao write -field=token auth/kubernetes/login role=admin jwt=-)" && [ -n "$BAO_TOKEN" ] || exit 1
export BAO_TOKEN; unset JWT
'

{
    kubectl --context "$context" -n openbao create token openbao-admin --duration=10m
    # Forward the caller's input, if any; a terminal has none to give.
    if [ ! -t 0 ]; then
        cat
    fi
} | kubectl --context "$context" -n openbao exec -i openbao-0 -c openbao -- sh -c "$login$script" sh "$@"
