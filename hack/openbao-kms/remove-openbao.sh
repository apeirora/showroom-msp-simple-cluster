#!/usr/bin/env bash
# Removes one cluster's KMS access from OpenBao, in the order that closes it
# fastest: destroy every secret ID of the role, then delete the role and the
# policy, then (optionally) the connection Secret. The shared mounts stay.
#
# --delete-key also deletes the cluster's Transit key. That is irreversible: every
# Secret the cluster ever wrote, including copies in etcd backups, becomes
# unreadable (crypto-shredding). Do it only after the cluster is gone, and repeat
# the key name with --confirm-key.
#
# Uses the caller's `bao` login, BAO_ADDR and BAO_NAMESPACE, like setup-openbao.sh.
#
# usage: remove-openbao.sh --cluster <id> [--secret <namespace>/<name> [--kubeconfig <path>]]
#                          [--transit-mount <path>] [--approle-mount <path>]
#                          [--delete-key --confirm-key <id>]
set -euo pipefail

cluster='' secret='' kubeconfig='' confirm='' delete_key=false
transit_mount=simplecluster-kms approle_mount=simplecluster-kms-approle

die() { echo "remove-openbao.sh: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case $1 in
    --cluster) cluster=$2; shift 2 ;;
    --secret) secret=$2; shift 2 ;;
    --kubeconfig) kubeconfig=$2; shift 2 ;;
    --transit-mount) transit_mount=$2; shift 2 ;;
    --approle-mount) approle_mount=$2; shift 2 ;;
    --delete-key) delete_key=true; shift ;;
    --confirm-key) confirm=$2; shift 2 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ $cluster =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]] || die "--cluster must be a DNS label"
! $delete_key || [ "$confirm" = "$cluster" ] || die "--delete-key needs --confirm-key $cluster"
[ -n "${BAO_ADDR:-}" ] || die "BAO_ADDR is not set"
[ -n "${BAO_NAMESPACE+x}" ] || die "set BAO_NAMESPACE explicitly (empty string for the root namespace)"
for tool in bao jq; do command -v $tool >/dev/null || die "$tool is required"; done
[ -z "$secret" ] || command -v kubectl >/dev/null || die "kubectl is required for --secret"

role=$cluster
key=$cluster
policy=simplecluster-kms-$cluster

echo "OpenBao ${BAO_ADDR}, namespace '${BAO_NAMESPACE}'"
bao token lookup -format=json >/dev/null 2>&1 || die "not logged in (bao token lookup failed)"

if bao read -format=json "auth/$approle_mount/role/$role" >/dev/null 2>&1; then
  n=0
  for acc in $(bao list -format=json "auth/$approle_mount/role/$role/secret-id" 2>/dev/null | jq -r '.[]?'); do
    bao write "auth/$approle_mount/role/$role/secret-id-accessor/destroy" secret_id_accessor="$acc" >/dev/null
    n=$((n + 1))
  done
  echo "destroyed $n secret ID(s) of role $role"
  bao delete "auth/$approle_mount/role/$role" >/dev/null
  echo "deleted role $role"
else
  echo "role $role does not exist"
fi

bao policy delete "$policy" >/dev/null
echo "deleted policy $policy"

if [ -n "$secret" ]; then
  ns=${secret%%/*} name=${secret#*/}
  [ -n "$ns" ] && [ -n "$name" ] && [ "$ns" != "$secret" ] || die "--secret must be <namespace>/<name>"
  kubectl ${kubeconfig:+--kubeconfig "$kubeconfig"} --request-timeout=30s \
    delete secret "$name" -n "$ns" --ignore-not-found >/dev/null
  echo "deleted Secret $ns/$name"
fi

if $delete_key; then
  if bao read -format=json "$transit_mount/keys/$key" >/dev/null 2>&1; then
    bao write "$transit_mount/keys/$key/config" deletion_allowed=true >/dev/null
    bao delete "$transit_mount/keys/$key" >/dev/null
    echo "deleted transit key $transit_mount/keys/$key; data encrypted with it is now unreadable"
  else
    echo "transit key $transit_mount/keys/$key does not exist"
  fi
else
  echo "kept transit key $transit_mount/keys/$key (use --delete-key to crypto-shred)"
fi
