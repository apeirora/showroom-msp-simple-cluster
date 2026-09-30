#!/usr/bin/env bash
# Configures OpenBao for one cluster's KMS plugin, idempotently: a Transit mount
# and key, a least-privilege policy, and an AppRole role. See docs/openbao-kms.md.
#
# Uses the caller's `bao` login and the environment's BAO_ADDR / BAO_NAMESPACE
# (BAO_NAMESPACE must be set explicitly; the script never creates namespaces).
# The secret ID is issued only when the destination has none yet or on
# --rotate-secret-id, and goes straight to a Kubernetes Secret or a 0600 file.
# Credentials are never printed.
#
# usage: setup-openbao.sh --cluster <id> (--secret <namespace>/<name> | --out-file <path>) [options]
#   --transit-mount <path>   default simplecluster-kms
#   --approle-mount <path>   default simplecluster-kms-approle
#   --token-period <dur>     periodic token period, default 1h
#   --secret-id-ttl <dur>    default 2160h (90 days)
#   --bound-cidrs <cidrs>    comma-separated; binds secret ID use and tokens to these sources
#   --ca-cert-file <path>    PEM to hand to the plugin when OpenBao's CA is not publicly trusted
#   --kubeconfig <path>      for --secret
#   --rotate-secret-id       issue a new secret ID even if the destination has one
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
cluster='' secret='' out_file='' ca_file='' kubeconfig='' bound_cidrs='' rotate=false
transit_mount=simplecluster-kms approle_mount=simplecluster-kms-approle
token_period=1h secret_id_ttl=2160h

die() { echo "setup-openbao.sh: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case $1 in
    --cluster) cluster=$2; shift 2 ;;
    --secret) secret=$2; shift 2 ;;
    --out-file) out_file=$2; shift 2 ;;
    --transit-mount) transit_mount=$2; shift 2 ;;
    --approle-mount) approle_mount=$2; shift 2 ;;
    --token-period) token_period=$2; shift 2 ;;
    --secret-id-ttl) secret_id_ttl=$2; shift 2 ;;
    --bound-cidrs) bound_cidrs=$2; shift 2 ;;
    --ca-cert-file) ca_file=$2; shift 2 ;;
    --kubeconfig) kubeconfig=$2; shift 2 ;;
    --rotate-secret-id) rotate=true; shift ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ $cluster =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]] || die "--cluster must be a DNS label"
[ -n "$secret" ] || [ -n "$out_file" ] || die "one of --secret or --out-file is required"
[ -z "$secret" ] || [ -z "$out_file" ] || die "--secret and --out-file are exclusive"
[ -n "${BAO_ADDR:-}" ] || die "BAO_ADDR is not set"
[ -n "${BAO_NAMESPACE+x}" ] || die "set BAO_NAMESPACE explicitly (empty string for the root namespace)"
case $BAO_ADDR in https://*) ;; *) die "BAO_ADDR must be https://" ;; esac
[ -z "$ca_file" ] || [ -r "$ca_file" ] || die "cannot read $ca_file"
for tool in bao jq; do command -v $tool >/dev/null || die "$tool is required"; done
[ -z "$secret" ] || command -v kubectl >/dev/null || die "kubectl is required for --secret"

key=$cluster
role=$cluster
policy=simplecluster-kms-$cluster
kc=(kubectl ${kubeconfig:+--kubeconfig "$kubeconfig"} --request-timeout=30s)

echo "OpenBao ${BAO_ADDR}, namespace '${BAO_NAMESPACE}'"
bao token lookup -format=json >/dev/null 2>&1 || die "not logged in (bao token lookup failed)"

# ---------------------------------------------------------------- transit
mount_type() { # <list-command> <path>
  bao "$1" list -format=json | jq -r --arg p "$2/" '.[$p].type // empty'
}
case $(mount_type secrets "$transit_mount") in
  "") bao secrets enable -path="$transit_mount" -description="SimpleCluster KMS keys" transit ;;
  transit) echo "transit mount $transit_mount/ exists" ;;
  *) die "$transit_mount/ exists but is not a transit mount" ;;
esac

if bao read -format=json "$transit_mount/keys/$key" >/dev/null 2>&1; then
  echo "transit key $transit_mount/keys/$key exists (left unchanged)"
else
  # Losing this key makes the cluster's Secrets unreadable: not exportable,
  # no plaintext backup, deletion not allowed. Rely on OpenBao backups.
  bao write -f "$transit_mount/keys/$key" type=aes256-gcm96 exportable=false allow_plaintext_backup=false >/dev/null
  echo "created transit key $transit_mount/keys/$key"
fi
bao read -format=json "$transit_mount/keys/$key" \
  | jq -c '.data | {type, exportable, allow_plaintext_backup, deletion_allowed, latest_version}'

# ----------------------------------------------------------------- policy
sed -e "s|@TRANSIT_MOUNT@|$transit_mount|g" -e "s|@TRANSIT_KEY@|$key|g" "$HERE/policy.hcl.tpl" \
  | bao policy write "$policy" - >/dev/null
echo "wrote policy $policy"

# ---------------------------------------------------------------- approle
case $(mount_type auth "$approle_mount") in
  "") bao auth enable -path="$approle_mount" -description="SimpleCluster KMS plugins" approle ;;
  approle) echo "approle mount $approle_mount/ exists" ;;
  *) die "$approle_mount/ exists but is not an approle mount" ;;
esac

# CIDRs are always written, so dropping --bound-cidrs clears them. A secret ID
# keeps the CIDRs it was issued with; changing them needs --rotate-secret-id.
old_cidrs=$(bao read -format=json "auth/$approle_mount/role/$role" 2>/dev/null \
  | jq -r '.data.secret_id_bound_cidrs // [] | join(",")' || true)
role_args=(
  token_policies="$policy" token_period="$token_period" token_ttl=0 token_max_ttl=0
  token_type=service secret_id_ttl="$secret_id_ttl" secret_id_num_uses=0
  secret_id_bound_cidrs="$bound_cidrs" token_bound_cidrs="$bound_cidrs"
)
bao write "auth/$approle_mount/role/$role" "${role_args[@]}" >/dev/null
if [ -n "$old_cidrs" ] && [ "$old_cidrs" != "$bound_cidrs" ] && ! $rotate; then
  echo "note: bound CIDRs changed from '$old_cidrs'; the existing secret ID keeps them until --rotate-secret-id"
fi
bao read -format=json "auth/$approle_mount/role/$role" \
  | jq -c '.data | {token_policies, token_period, secret_id_ttl, secret_id_num_uses, secret_id_bound_cidrs}'
role_id=$(bao read -field=role_id "auth/$approle_mount/role/$role/role-id")

# ------------------------------------------------------------- destination
# Non-secret connection data, as `key=<base64>` lines (install.sh reads this form).
b64() { printf '%s' "$1" | base64 | tr -d '\n'; }
connection_lines() {
  echo "address=$(b64 "$BAO_ADDR")"
  [ -z "$BAO_NAMESPACE" ] || echo "namespace=$(b64 "$BAO_NAMESPACE")"
  echo "transitMount=$(b64 "$transit_mount")"
  echo "transitKey=$(b64 "$key")"
  echo "approleMount=$(b64 "$approle_mount")"
  echo "roleID=$(b64 "$role_id")"
  [ -z "$ca_file" ] || echo "caCert=$(base64 < "$ca_file" | tr -d '\n')"
}

has_secret_id=false
if [ -n "$secret" ]; then
  ns=${secret%%/*} name=${secret#*/}
  [ -n "$ns" ] && [ -n "$name" ] && [ "$ns" != "$secret" ] || die "--secret must be <namespace>/<name>"
  if [ -n "$("${kc[@]}" get secret "$name" -n "$ns" --ignore-not-found -o jsonpath='{.data.secretID}')" ]; then
    has_secret_id=true
  fi
elif [ -s "$out_file" ] && grep -q '^secretID=' "$out_file"; then
  has_secret_id=true
fi

issue=false
if ! $has_secret_id || $rotate; then issue=true; fi

old_accessor=
secret_lines=
if $issue; then
  resp=$(bao write -format=json -f "auth/$approle_mount/role/$role/secret-id")
  secret_lines="secretID=$(jq -r '.data.secret_id' <<<"$resp" | base64 | tr -d '\n')
secretIDAccessor=$(jq -r '.data.secret_id_accessor' <<<"$resp" | base64 | tr -d '\n')"
  unset resp
fi

if [ -n "$secret" ]; then
  if $issue && $has_secret_id; then
    old_accessor=$("${kc[@]}" get secret "$name" -n "$ns" -o jsonpath='{.data.secretIDAccessor}' | base64 -d)
  fi
  # The data map travels through pipes only; unissued keys keep their values.
  data_json=$({ connection_lines; [ -z "$secret_lines" ] || echo "$secret_lines"; } \
    | jq -R -s 'split("\n") | map(select(length > 0) | capture("^(?<key>[^=]+)=(?<value>.*)$")) | from_entries')
  if "${kc[@]}" get secret "$name" -n "$ns" >/dev/null 2>&1; then
    printf '%s' "$data_json" | jq '{data: .}' \
      | "${kc[@]}" patch secret "$name" -n "$ns" --type merge --patch-file /dev/stdin >/dev/null
    echo "updated Secret $ns/$name"
  else
    printf '%s' "$data_json" | jq --arg ns "$ns" --arg name "$name" \
      '{apiVersion: "v1", kind: "Secret", type: "Opaque",
        metadata: {name: $name, namespace: $ns, labels: {"showroom.apeirora.eu/purpose": "openbao-kms"}},
        data: .}' \
      | "${kc[@]}" create -f - >/dev/null
    echo "created Secret $ns/$name"
  fi
  unset data_json
else
  if $issue && $has_secret_id; then
    old_accessor=$(sed -n 's/^secretIDAccessor=//p' "$out_file" | base64 -d)
  fi
  tmp=$(mktemp "$(dirname "$out_file")/.openbao-kms.XXXXXX")
  { connection_lines
    if [ -n "$secret_lines" ]; then echo "$secret_lines"
    else grep -E '^secretID(Accessor)?=' "$out_file"; fi
  } > "$tmp"
  chmod 0600 "$tmp"
  mv -f "$tmp" "$out_file"
  echo "wrote $out_file (0600)"
fi
unset secret_lines

if $issue; then echo "issued a new secret ID"; else echo "kept the existing secret ID"; fi
if [ -n "$old_accessor" ]; then
  echo "previous secret ID is still valid until its TTL. After every host runs with the new one, revoke it:"
  echo "  bao write auth/$approle_mount/role/$role/secret-id-accessor/destroy secret_id_accessor=$old_accessor"
fi
