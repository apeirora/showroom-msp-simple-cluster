#!/usr/bin/env bash
# Pull the operator-generated private key out of the cluster and SSH
# into one host of an OpenStackVMSet.
#
# Usage:
#   ./ssh.sh <vmset-name>                      # SSH as eouser into host[0]
#   ./ssh.sh -u ubuntu <vmset-name>            # different SSH user
#   ./ssh.sh -i 2 <vmset-name>                 # host index in status.groups[0].hosts
#   ./ssh.sh -g workers <vmset-name>           # named group instead of groups[0]
#   ./ssh.sh -n team-a <vmset-name>            # CR in another namespace
#   ./ssh.sh -p <vmset-name>                   # print, don't connect
#   ./ssh.sh <vmset-name> -- whoami            # everything after `--` is the remote command
#
# Environment overrides (same as flags): NAMESPACE, SSH_LOGIN, HOST_INDEX, GROUP.
set -euo pipefail

NS="${NAMESPACE:-default}"
SSH_LOGIN="${SSH_LOGIN:-eouser}"
INDEX="${HOST_INDEX:-0}"
GROUP="${GROUP:-}"
PRINT_ONLY=0

while getopts ":n:u:i:g:p" opt; do
    case "$opt" in
        n) NS="$OPTARG" ;;
        u) SSH_LOGIN="$OPTARG" ;;
        i) INDEX="$OPTARG" ;;
        g) GROUP="$OPTARG" ;;
        p) PRINT_ONLY=1 ;;
        *) echo "unknown flag -$OPTARG" >&2; exit 2 ;;
    esac
done
shift $((OPTIND - 1))

if [[ $# -lt 1 ]]; then
    echo "usage: $0 [-n ns] [-u user] [-i idx] [-g group] [-p] <vmset-name> [-- remote-cmd...]" >&2
    exit 2
fi

NAME="$1"; shift

REMOTE_CMD=()
if [[ $# -gt 0 ]]; then
    # Strip the optional `--` separator if present.
    if [[ "$1" == "--" ]]; then shift; fi
    REMOTE_CMD=("$@")
fi

# Resolve the group: -g if provided, otherwise status.groups[0].
if [[ -z "$GROUP" ]]; then
    GROUP_NAME=$(kubectl -n "$NS" get osvms "$NAME" -o jsonpath='{.status.groups[0].name}')
else
    GROUP_NAME="$GROUP"
fi
if [[ -z "$GROUP_NAME" ]]; then
    PHASE=$(kubectl -n "$NS" get osvms "$NAME" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    echo "no group on OpenStackVMSet $NS/$NAME yet (phase=${PHASE:-<unknown>})" >&2
    exit 1
fi

HOST_NAME=$(kubectl -n "$NS" get osvms "$NAME" \
    -o jsonpath="{.status.groups[?(@.name=='$GROUP_NAME')].hosts[$INDEX].name}")
FIP=$(kubectl -n "$NS" get osvms "$NAME" \
    -o jsonpath="{.status.groups[?(@.name=='$GROUP_NAME')].hosts[$INDEX].floatingIP}")
INTERNAL=$(kubectl -n "$NS" get osvms "$NAME" \
    -o jsonpath="{.status.groups[?(@.name=='$GROUP_NAME')].hosts[$INDEX].internalIP}")
STATUS=$(kubectl -n "$NS" get osvms "$NAME" \
    -o jsonpath="{.status.groups[?(@.name=='$GROUP_NAME')].hosts[$INDEX].status}")

if [[ -z "$HOST_NAME" ]]; then
    echo "no host at index $INDEX in group $GROUP_NAME" >&2
    exit 1
fi

ADDR="${FIP:-$INTERNAL}"
if [[ -z "$ADDR" ]]; then
    echo "host $HOST_NAME has no floating or internal IP yet (status=$STATUS)" >&2
    exit 1
fi

SECRET_NAME=$(kubectl -n "$NS" get osvms "$NAME" -o jsonpath='{.status.sshSecretRef.name}')
if [[ -z "$SECRET_NAME" ]]; then
    echo "OpenStackVMSet $NS/$NAME has no sshSecretRef yet" >&2
    exit 1
fi

KEY_FILE="${TMPDIR:-/tmp}/${NS}-${NAME}-id_ed25519"
KNOWN_HOSTS="${TMPDIR:-/tmp}/${NS}-${NAME}-known_hosts"
kubectl -n "$NS" get secret "$SECRET_NAME" -o jsonpath='{.data.ssh-privatekey}' \
    | base64 -d > "$KEY_FILE"
chmod 600 "$KEY_FILE"

cat >&2 <<EOF
=== OpenStackVMSet $NS/$NAME ===
group:        $GROUP_NAME
host[$INDEX]:    $HOST_NAME ($STATUS)
address:      $ADDR  (floating=${FIP:-<none>}  internal=${INTERNAL:-<none>})
key file:     $KEY_FILE
EOF

# -F /dev/null avoids picking up the user's system-wide ssh_config (some
# distros ship unreadable Include directories). accept-new is TOFU on
# first connect, persisted in our own known_hosts file.
SSH_CMD=(ssh
    -F /dev/null
    -i "$KEY_FILE"
    -o StrictHostKeyChecking=accept-new
    -o UserKnownHostsFile="$KNOWN_HOSTS"
    "$SSH_LOGIN@$ADDR"
)
if [[ "${#REMOTE_CMD[@]}" -gt 0 ]]; then
    SSH_CMD+=("${REMOTE_CMD[@]}")
fi

if [[ "$PRINT_ONLY" -eq 1 ]]; then
    echo
    printf '%q ' "${SSH_CMD[@]}"
    echo
    exit 0
fi

echo "=== connecting ===" >&2
exec "${SSH_CMD[@]}"
