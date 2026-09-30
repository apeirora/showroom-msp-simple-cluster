#!/usr/bin/env bash
# Installs or updates the KMS plugin on a control-plane host from the connection
# Secret written by setup-openbao.sh. Run it before k3s is installed on that host.
# The Secret's data stays base64 on this machine and is decoded only by
# install.sh on the host, as root. See docs/openbao-kms.md.
#
# With --wrap-ttl the host does not get the Secret's secret ID. Instead this
# script mints a new one for this host, response-wrapped (single use, valid for
# the given TTL), with the caller's `bao` login; install.sh unwraps it on the host.
# The Secret then needs no secret ID at all (setup-openbao.sh --no-secret-id).
#
# usage: push-to-host.sh --secret <namespace>/<name> --host <user@host> [--kubeconfig <path>]
#                        [--wrap-ttl <dur>] [-- <ssh options>]
# PUSH_TO_HOST_SSH replaces the ssh command (the e2e test runs on the host itself).
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
secret='' host='' kubeconfig='' wrap_ttl=''
ssh_opts=()
ssh_cmd=${PUSH_TO_HOST_SSH:-ssh}

die() { echo "push-to-host.sh: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case $1 in
    --secret) secret=$2; shift 2 ;;
    --host) host=$2; shift 2 ;;
    --kubeconfig) kubeconfig=$2; shift 2 ;;
    --wrap-ttl) wrap_ttl=$2; shift 2 ;;
    --) shift; ssh_opts=("$@"); break ;;
    *) die "unknown argument: $1" ;;
  esac
done
[ -n "$host" ] || die "--host is required"
ns=${secret%%/*} name=${secret#*/}
[ -n "$ns" ] && [ -n "$name" ] && [ "$ns" != "$secret" ] || die "--secret must be <namespace>/<name>"
for tool in kubectl jq "$ssh_cmd"; do command -v $tool >/dev/null || die "$tool is required"; done
[ -z "$wrap_ttl" ] || command -v bao >/dev/null || die "bao is required for --wrap-ttl"

kc=(kubectl ${kubeconfig:+--kubeconfig "$kubeconfig"} --request-timeout=30s)
data=$("${kc[@]}" get secret "$name" -n "$ns" -o json | jq -c '.data')
field() { jq -r --arg k "$1" '.[$k] // empty' <<<"$data" | base64 -d; }

if [ -z "$wrap_ttl" ]; then
  [ -n "$(jq -r '.secretID // empty' <<<"$data")" ] \
    || die "Secret $ns/$name has no secretID; run setup-openbao.sh, or use --wrap-ttl"
  lines=$(jq -r 'to_entries[] | "\(.key)=\(.value)"' <<<"$data")
else
  role=$(field approleRole)
  [ -n "$role" ] || die "Secret $ns/$name has no approleRole; re-run setup-openbao.sh"
  # Mint against the OpenBao and namespace the Secret names, whatever the shell says.
  wrap=$(BAO_ADDR=$(field address) BAO_NAMESPACE=$(field namespace) \
    bao write -format=json -wrap-ttl="$wrap_ttl" -f "auth/$(field approleMount)/role/$role/secret-id" \
    | jq -r '.wrap_info.token // empty') || die "could not mint a wrapped secret ID (bao login?)"
  [ -n "$wrap" ] || die "OpenBao returned no wrapping token"
  lines=$(jq -r 'del(.secretID, .secretIDAccessor) | to_entries[] | "\(.key)=\(.value)"' <<<"$data")
  lines+=$'\n'"secretIDWrapped=$(printf '%s' "$wrap" | base64 | tr -d '\n')"
  unset wrap
  echo "minted a wrapped secret ID for $host, valid for $wrap_ttl and one use"
fi
unset data

# Stage the installer (not secret) in a private temp dir on the host.
remote=$("$ssh_cmd" ${ssh_opts[@]+"${ssh_opts[@]}"} "$host" 'd=$(mktemp -d) && cat > "$d/install.sh" && echo "$d"' < "$HERE/install.sh")
[[ $remote =~ ^/[[:alnum:]/._-]+$ ]] || die "unexpected staging path from host"

# Stream the data (values still base64) to the installer's stdin.
printf '%s\n' "$lines" \
  | "$ssh_cmd" ${ssh_opts[@]+"${ssh_opts[@]}"} "$host" "sudo bash '$remote/install.sh'; rc=\$?; rm -rf '$remote'; exit \$rc"
