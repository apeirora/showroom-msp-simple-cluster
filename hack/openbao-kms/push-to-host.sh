#!/usr/bin/env bash
# Installs or updates the KMS plugin on a control-plane host from the connection
# Secret written by setup-openbao.sh. Run it before k3s is installed on that host.
# The Secret's data stays base64 on this machine and is decoded only by
# install.sh on the host, as root. See docs/openbao-kms.md.
#
# usage: push-to-host.sh --secret <namespace>/<name> --host <user@host> [--kubeconfig <path>] [-- <ssh options>]
# PUSH_TO_HOST_SSH replaces the ssh command (the e2e test runs on the host itself).
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
secret='' host='' kubeconfig=''
ssh_opts=()
ssh_cmd=${PUSH_TO_HOST_SSH:-ssh}

die() { echo "push-to-host.sh: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case $1 in
    --secret) secret=$2; shift 2 ;;
    --host) host=$2; shift 2 ;;
    --kubeconfig) kubeconfig=$2; shift 2 ;;
    --) shift; ssh_opts=("$@"); break ;;
    *) die "unknown argument: $1" ;;
  esac
done
[ -n "$host" ] || die "--host is required"
ns=${secret%%/*} name=${secret#*/}
[ -n "$ns" ] && [ -n "$name" ] && [ "$ns" != "$secret" ] || die "--secret must be <namespace>/<name>"
for tool in kubectl jq "$ssh_cmd"; do command -v $tool >/dev/null || die "$tool is required"; done

kc=(kubectl ${kubeconfig:+--kubeconfig "$kubeconfig"} --request-timeout=30s)
"${kc[@]}" get secret "$name" -n "$ns" -o jsonpath='{.data.secretID}' | grep -q . \
  || die "Secret $ns/$name has no secretID; run setup-openbao.sh first"

# Stage the installer (not secret) in a private temp dir on the host.
remote=$("$ssh_cmd" ${ssh_opts[@]+"${ssh_opts[@]}"} "$host" 'd=$(mktemp -d) && cat > "$d/install.sh" && echo "$d"' < "$HERE/install.sh")
[[ $remote =~ ^/[[:alnum:]/._-]+$ ]] || die "unexpected staging path from host"

# Stream the Secret's data (still base64) to the installer's stdin.
"${kc[@]}" get secret "$name" -n "$ns" -o json \
  | jq -r '.data | to_entries[] | "\(.key)=\(.value)"' \
  | "$ssh_cmd" ${ssh_opts[@]+"${ssh_opts[@]}"} "$host" "sudo bash '$remote/install.sh'; rc=\$?; rm -rf '$remote'; exit \$rc"
