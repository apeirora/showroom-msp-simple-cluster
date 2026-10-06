#!/usr/bin/env bash
# Checks on a k3s control-plane host that the kube-apiserver encrypts Secrets
# with the OpenBao KMS plugin. Run as root. Prints one PASS/FAIL line per check,
# exits non-zero on any failure; the output is meant to be kept as evidence.
#
# It creates one synthetic Secret (kube-system/kms-verify-<random>) and deletes it
# on exit. --restart also restarts k3s once and reads the Secret back afterwards.
# Reading etcd needs etcdctl: a pinned, checksum-verified release is fetched into
# a temp dir unless ETCDCTL points at one.
#
# usage: verify-host.sh [--restart]
set -euo pipefail

ETCD_VER=v3.6.5
declare -A ETCD_SHA256=(
  [amd64]=66bad39ed920f6fc15fd74adcb8bfd38ba9a6412f8c7852d09eb11670e88cac3
  [arm64]=7010161787077b07de29b15b76825ceacbbcedcb77fe2e6832f509be102cab6b
)
TLS=/var/lib/rancher/k3s/server/tls/etcd
PREFIX='k8s:enc:kms:v2:openbao:'
HEALTH_PORT=10271

restart=false
[ "${1:-}" != --restart ] || restart=true
[ "$(id -u)" -eq 0 ] || { echo "verify-host.sh: run as root" >&2; exit 2; }
command -v kubectl >/dev/null || { echo "verify-host.sh: kubectl (k3s) not found" >&2; exit 2; }

fails=0
pass() { printf 'PASS  %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*"; fails=$((fails + 1)); }
check() { # <description> <command...>
  local what=$1; shift
  if "$@"; then pass "$what"; else fail "$what"; fi
}

tmp=$(mktemp -d)
name=
cleanup() {
  [ -z "$name" ] || kubectl delete secret "$name" -n kube-system --ignore-not-found >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

etcdctl_bin=${ETCDCTL:-}
if [ -z "$etcdctl_bin" ]; then
  case $(uname -m) in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) echo "unsupported architecture" >&2; exit 2 ;; esac
  tgz=etcd-$ETCD_VER-linux-$arch.tar.gz
  curl -fsSL -o "$tmp/$tgz" "https://github.com/etcd-io/etcd/releases/download/$ETCD_VER/$tgz"
  echo "${ETCD_SHA256[$arch]}  $tmp/$tgz" | sha256sum -c --quiet - || { echo "etcd checksum mismatch" >&2; exit 2; }
  tar -xzf "$tmp/$tgz" -C "$tmp" --strip-components=1 "etcd-$ETCD_VER-linux-$arch/etcdctl"
  etcdctl_bin=$tmp/etcdctl
fi
ectl() {
  "$etcdctl_bin" --endpoints=https://127.0.0.1:2379 --cacert="$TLS/server-ca.crt" \
    --cert="$TLS/client.crt" --key="$TLS/client.key" "$@"
}
prefix_of() { ectl get "$1" --print-value-only | head -c ${#PREFIX} || true; }
has_line() { grep -q -F -- "$2" <<<"$1"; }

echo "$(hostname) · $(k3s --version | head -1) · $(/usr/local/bin/vault-kubernetes-kms -version 2>/dev/null || echo 'plugin missing')"

# ---------------------------------------------------------------- the plugin
uid=$(ps -o uid= -C vault-kubernetes-kms | tr -d ' ' | head -1)
after=$(systemctl show -p After k3s)
readyz=$(kubectl get --raw='/readyz?verbose' 2>&1 || true)
check "plugin unit is active" systemctl is-active --quiet vault-kubernetes-kms
check "plugin runs unprivileged (uid ${uid:-none})" test -n "$uid" -a "$uid" != 0
check "k3s is ordered after the plugin" has_line "$after" vault-kubernetes-kms.service
check "env file is 0600 root" test "$(stat -c '%a %U' /etc/vault-kms/env)" = "600 root"
check "plugin health endpoint answers OK" curl -fsS -o /dev/null "http://127.0.0.1:$HEALTH_PORT/health"
check "kube-apiserver reports kms-providers ok" has_line "$readyz" "[+]kms-providers ok"

# ---------------------------------------------------------- a synthetic Secret
marker=kms-verify-plaintext-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')
name=kms-verify-$(od -An -tx1 -N4 /dev/urandom | tr -d ' \n')
kubectl create secret generic "$name" -n kube-system --from-literal=value="$marker" >/dev/null
key=/registry/secrets/kube-system/$name
check "synthetic Secret is stored with prefix $PREFIX" test "$(prefix_of "$key")" = "$PREFIX"
# Grep a file, not a pipe: under pipefail an early grep match can SIGPIPE etcdctl
# and a negated pipeline would then report a leak as a pass.
ectl get "$key" --print-value-only > "$tmp/value"
check "its plaintext is absent from etcd" test -s "$tmp/value" -a "$(grep -c -a -F -- "$marker" "$tmp/value" || true)" = 0
check "it reads back through the API" test "$(kubectl get secret "$name" -n kube-system -o jsonpath='{.data.value}' | base64 -d)" = "$marker"

# --------------------------------------------------------------- all Secrets
total=0 enc=0
while read -r k; do
  [ -n "$k" ] || continue
  total=$((total + 1))
  [ "$(prefix_of "$k")" != "$PREFIX" ] || enc=$((enc + 1))
done < <(ectl get /registry/secrets/ --prefix --keys-only)
check "$enc of $total Secrets in etcd are KMS v2 ciphertext" test "$total" -gt 0 -a "$enc" -eq "$total"

# -------------------------------------------------------- credential hygiene
sid=$(sed -n 's/^VAULT_KMS_APPROLE_SECRET_ID=//p' /etc/vault-kms/env)
n=$({ journalctl --no-pager -u vault-kubernetes-kms -u k3s; systemctl cat vault-kubernetes-kms k3s
      systemctl show vault-kubernetes-kms k3s; cat /etc/rancher/k3s/config.yaml.d/*; ps -eo args; } 2>/dev/null \
    | { grep -c -F -f <(printf '%s\n' "$sid") || true; })
unset sid
check "secret ID absent from journal, units, k3s config and process list" test "$n" = 0

# ------------------------------------------------------------------ restart
if $restart; then
  systemctl restart k3s
  for _ in $(seq 1 90); do kubectl get --raw=/readyz >/dev/null 2>&1 && break; sleep 2; done
  check "synthetic Secret reads back after a k3s restart" \
    test "$(kubectl get secret "$name" -n kube-system -o jsonpath='{.data.value}' | base64 -d)" = "$marker"
fi

echo
if [ "$fails" -eq 0 ]; then echo "ALL CHECKS PASSED"; else echo "$fails CHECK(S) FAILED"; exit 1; fi
