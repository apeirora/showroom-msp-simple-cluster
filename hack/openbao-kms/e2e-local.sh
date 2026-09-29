#!/usr/bin/env bash
# End-to-end check of the OpenBao KMS integration on one simulated control-plane
# host. OpenBao runs in dev-TLS mode; an Ubuntu container with systemd plays the
# VM, where setup-openbao.sh, install.sh and the stock k3s installer run the way
# simple-cluster-operator-ubuntu runs them over SSH.
#
# Needs Docker with privileged containers and internet access (k3s installer,
# plugin release, images). Removes everything it created unless KEEP=1.
#
# usage: hack/openbao-kms/e2e-local.sh
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
P=${E2E_PREFIX:-openbao-kms-e2e}
PLATFORM=linux/$(docker version --format '{{.Server.Arch}}')
BAO_IMG=quay.io/openbao/openbao:2.5.5
ETCD_IMG=registry.k8s.io/etcd:3.6.5-0
K3S_VERSION=${K3S_VERSION:-v1.36.4+k3s1}
NS=msp/pm-cc-d2
CLUSTER=e2e
TRANSIT=simplecluster-kms
APPROLE=simplecluster-kms-approle
ROOT_TOKEN=$(od -An -tx1 -N24 /dev/urandom | tr -d ' \n')

cleanup() {
  if [ "${KEEP:-}" = 1 ]; then echo "KEEP=1: leaving $P-* in place"; return; fi
  docker rm -f "$P-host" "$P-openbao" >/dev/null 2>&1 || true
  docker volume rm "$P-k3s" >/dev/null 2>&1 || true
  docker network rm "$P" >/dev/null 2>&1 || true
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
pass() { printf 'PASS  %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*" >&2; exit 1; }
on_host() { docker exec -i "$P-host" bash -euo pipefail -c "$1"; }
as_root_bao() { # bao on the host with the dev root token, in the test namespace
  docker exec -i -e BAO_ADDR="https://$P-openbao:8200" -e BAO_CACERT=/opt/openbao-ca.pem \
    -e BAO_TOKEN="$ROOT_TOKEN" -e BAO_NAMESPACE="${BAO_NS-$NS}" "$P-host" bao "$@"
}
etcd_get() {
  docker run --rm --platform "$PLATFORM" --network "container:$P-host" -v "$P-k3s:/k3s:ro" "$ETCD_IMG" \
    etcdctl --endpoints=https://127.0.0.1:2379 --cacert=/k3s/server/tls/etcd/server-ca.crt \
    --cert=/k3s/server/tls/etcd/client.crt --key=/k3s/server/tls/etcd/client.key \
    "$@" </dev/null
}
wait_apiserver() {
  for _ in $(seq 1 120); do
    on_host 'kubectl get --raw=/readyz >/dev/null 2>&1' && return 0
    sleep 2
  done
  fail "kube-apiserver not ready after 240s"
}
k3s_pid() { on_host 'systemctl show -p MainPID --value k3s'; }
expect_denied() { # <label> <bao args...> as the plugin's AppRole token
  local label=$1 out; shift
  if out=$(docker exec -i -e BAO_ADDR="https://$P-openbao:8200" -e BAO_CACERT=/opt/openbao-ca.pem \
      -e BAO_NAMESPACE="$NS" "$P-host" bash -c 'BAO_TOKEN=$(cat /root/approle-token) bao "$@"' _ "$@" 2>&1); then
    fail "$label: allowed"
  elif grep -q -e 'Code: 403' -e 'permission denied' <<<"$out"; then
    pass "$label: denied"
  else
    fail "$label: unexpected error: $(tail -1 <<<"$out")"
  fi
}

step "images and network ($PLATFORM, k3s $K3S_VERSION)"
docker build -q --platform "$PLATFORM" -t "$P-host:latest" - >/dev/null <<'EOF'
FROM ubuntu:24.04
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      systemd systemd-sysv dbus curl ca-certificates jq iptables kmod sudo \
 && apt-get clean && rm -rf /var/lib/apt/lists/* \
 && systemctl mask getty.target console-getty.service systemd-udevd.service \
      systemd-logind.service systemd-resolved.service
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
EOF
docker pull -q --platform "$PLATFORM" "$BAO_IMG" >/dev/null
docker pull -q --platform "$PLATFORM" "$ETCD_IMG" >/dev/null
docker network create "$P" >/dev/null

step "OpenBao (dev, TLS) with namespace $NS"
docker run -d --platform "$PLATFORM" --name "$P-openbao" --hostname "$P-openbao" --network "$P" \
  --cap-add IPC_LOCK --env-file <(printf 'BAO_DEV_ROOT_TOKEN_ID=%s\n' "$ROOT_TOKEN") \
  "$BAO_IMG" server -dev -dev-tls -dev-tls-cert-dir=/tmp -dev-listen-address=0.0.0.0:8200 >/dev/null
for _ in $(seq 1 30); do docker exec "$P-openbao" test -f /tmp/vault-ca.pem 2>/dev/null && break; sleep 1; done

step "control-plane host (Ubuntu 24.04 + systemd)"
docker run -d --platform "$PLATFORM" --name "$P-host" --hostname "$P-host" --network "$P" \
  --privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock \
  -v "$P-k3s:/var/lib/rancher/k3s" "$P-host:latest" >/dev/null
for _ in $(seq 1 60); do
  state=$(docker exec "$P-host" systemctl is-system-running 2>/dev/null || true)
  case $state in running|degraded) break ;; esac
  sleep 1
done
docker cp -q "$P-openbao:/tmp/vault-ca.pem" - | docker cp -q - "$P-host:/opt/"
on_host 'mv /opt/vault-ca.pem /opt/openbao-ca.pem'
docker cp -q "$P-openbao:/bin/bao" - | docker cp -q - "$P-host:/usr/local/bin/"
docker cp -q "$HERE" "$P-host:/opt/openbao-kms"
BAO_NS='' as_root_bao namespace create msp >/dev/null
BAO_NS=msp as_root_bao namespace create pm-cc-d2 >/dev/null
pass "systemd $state, OpenBao reachable over verified TLS"

step "setup-openbao.sh (twice: must be idempotent)"
setup_with() {
  docker exec -i -e BAO_ADDR="https://$P-openbao:8200" -e BAO_CACERT=/opt/openbao-ca.pem \
    -e BAO_TOKEN="$ROOT_TOKEN" -e BAO_NAMESPACE="$NS" "$P-host" \
    bash /opt/openbao-kms/setup-openbao.sh --cluster "$CLUSTER" \
    --ca-cert-file /opt/openbao-ca.pem --token-period 2m "$@"
}
setup() { setup_with --out-file /root/kms-connection; }
setup
on_host 'test "$(stat -c %a /root/kms-connection)" = 600' || fail "connection file is not 0600"
first=$(on_host 'sha256sum /root/kms-connection')
out=$(setup)
grep -q 'kept the existing secret ID' <<<"$out" || fail "second run did not keep the secret ID"
[ "$first" = "$(on_host 'sha256sum /root/kms-connection')" ] || fail "second run changed the connection file"
pass "second run changed nothing"

step "install.sh, before k3s exists"
on_host 'bash /opt/openbao-kms/install.sh < /root/kms-connection'
on_host 'test "$(stat -c "%a %U" /etc/vault-kms/env)" = "600 root"' || fail "env file is not 0600 root"
uid=$(on_host 'ps -o uid= -C vault-kubernetes-kms | tr -d " "')
[ -n "$uid" ] && [ "$uid" != 0 ] || fail "plugin runs as uid '$uid'"
pass "plugin active as dynamic uid $uid; env file 0600 root"
on_host 'bash /opt/openbao-kms/install.sh < /root/kms-connection' >/dev/null
pass "re-run without changes succeeds"

step "k3s via get.k3s.io, as the operator installs it"
on_host "curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='$K3S_VERSION' sh -s - server --cluster-init --disable=traefik --disable=metrics-server" \
  | tail -2
wait_apiserver
on_host 'systemctl show -p After -p Wants k3s' | grep -q vault-kubernetes-kms.service \
  || fail "k3s.service is not ordered after the plugin"
on_host 'kubectl get --raw="/readyz?verbose"' | grep -q '\[+\]kms-providers ok' || fail "kms-providers not ok"
pass "$(on_host 'k3s --version | head -1'); k3s ordered after the plugin; kms-providers ok"

step "Secrets are KMS v2 ciphertext in etcd"
marker="e2e-plaintext-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')"
on_host "kubectl create secret generic e2e-secret -n default --from-literal=value=$marker" >/dev/null
etcd_get get /registry/secrets/default/e2e-secret --print-value-only | head -c 23 | grep -qx 'k8s:enc:kms:v2:openbao:' \
  || fail "e2e-secret is not KMS v2 ciphertext"
if etcd_get get /registry/secrets/default/e2e-secret --print-value-only | grep -q -a -- "$marker"; then
  fail "plaintext found in etcd"
fi
[ "$(on_host 'kubectl get secret e2e-secret -n default -o jsonpath={.data.value} | base64 -d')" = "$marker" ] \
  || fail "API read-back mismatch"
total=0 enc=0
while read -r key; do
  [ -n "$key" ] || continue
  total=$((total + 1))
  if etcd_get get "$key" --print-value-only | head -c 23 | grep -qx 'k8s:enc:kms:v2:openbao:'; then enc=$((enc + 1)); fi
done < <(etcd_get get /registry/secrets/ --prefix --keys-only)
[ "$total" -gt 0 ] && [ "$enc" -eq "$total" ] || fail "$enc of $total Secrets encrypted"
pass "ciphertext prefix, no plaintext, read-back ok; $enc/$total Secrets encrypted"

step "least privilege of the plugin's AppRole"
on_host 'field() { sed -n "s/^$1=//p" /root/kms-connection | base64 -d; }
  roleID=$(field roleID)
  field secretID | BAO_ADDR=https://'"$P"'-openbao:8200 BAO_CACERT=/opt/openbao-ca.pem BAO_NAMESPACE='"$NS"' \
    bao write -field=token auth/'"$APPROLE"'/login role_id="$roleID" secret_id=- > /root/approle-token'
as_root_bao write -f "$TRANSIT/keys/other" >/dev/null
expect_denied "encrypt with another cluster's key" write "$TRANSIT/encrypt/other" plaintext=aGk=
expect_denied "delete own key" delete "$TRANSIT/keys/$CLUSTER"
expect_denied "export own key" read "$TRANSIT/export/encryption-key/$CLUSTER"
expect_denied "rotate own key" write -f "$TRANSIT/keys/$CLUSTER/rotate"
expect_denied "list mounts" secrets list
on_host 'rm -f /root/approle-token'

step "credentials stay out of logs, units and process arguments"
leaks=$(on_host 'secretID=$(sed -n "s/^secretID=//p" /root/kms-connection | base64 -d)
  { journalctl --no-pager -u vault-kubernetes-kms -u k3s; systemctl cat vault-kubernetes-kms k3s;
    systemctl show vault-kubernetes-kms k3s; cat /etc/rancher/k3s/config.yaml.d/*; ps -eo args; } 2>/dev/null \
    | grep -c -F -f <(printf "%s\n" "$secretID") || true')
[ "$leaks" = 0 ] || fail "secret ID found $leaks time(s)"
pass "secret ID absent from journal, unit files, systemctl show, k3s config and ps"

step "plugin restart does not restart k3s"
before=$(k3s_pid)
on_host 'systemctl restart vault-kubernetes-kms'
[ "$(k3s_pid)" = "$before" ] || fail "k3s restarted with the plugin"
on_host 'kubectl get secret e2e-secret -n default >/dev/null'
pass "k3s PID $before unchanged; Secrets readable"

step "k3s restart"
on_host 'systemctl restart k3s'
wait_apiserver
on_host 'kubectl get secret e2e-secret -n default >/dev/null && kubectl create secret generic e2e-after-restart -n default --from-literal=value=x >/dev/null'
pass "old Secret readable, new Secret written"

step "OpenBao unavailable: fail closed, then recover"
docker pause "$P-openbao" >/dev/null
on_host 'systemctl restart --no-block k3s'
sleep 45
if err=$(on_host 'kubectl create secret generic e2e-while-down -n default --from-literal=value=x --request-timeout=20s' 2>&1); then
  docker unpause "$P-openbao" >/dev/null
  fail "Secret write succeeded while OpenBao was unavailable"
fi
echo "      write error: $(tail -1 <<<"$err")"
[ -z "$(etcd_get get /registry/secrets/default/e2e-while-down --keys-only)" ] || fail "Secret reached etcd"
pass "write rejected, nothing stored"
docker unpause "$P-openbao" >/dev/null
wait_apiserver
on_host 'kubectl get secret e2e-secret -n default >/dev/null && kubectl create secret generic e2e-after-recovery -n default --from-literal=value=x >/dev/null'
pass "recovered without intervention"

step "connection Secret, push-to-host.sh and secret ID rotation"
# The test cluster doubles as the management cluster; "ssh" runs on the host itself.
on_host 'printf "#!/bin/bash\nexec bash -c \"\${@: -1}\"\n" > /usr/local/bin/local-ssh && chmod +x /usr/local/bin/local-ssh'
push() { on_host "PUSH_TO_HOST_SSH=local-ssh bash /opt/openbao-kms/push-to-host.sh --secret default/$CLUSTER-openbao-kms --host localhost" >/dev/null; }
secret_sha() { on_host "kubectl get secret $CLUSTER-openbao-kms -n default -o jsonpath={.data.secretID} | sha256sum"; }
setup_secret() { setup_with --secret "default/$CLUSTER-openbao-kms" "$@"; }
before=$(k3s_pid)
setup_secret | grep -q 'created Secret' || fail "Secret not created"
keys=$(on_host "kubectl get secret $CLUSTER-openbao-kms -n default -o json | jq -r '.data | keys | join(\",\")'")
[ "$keys" = "address,approleMount,caCert,namespace,roleID,secretID,secretIDAccessor,transitKey,transitMount" ] \
  || fail "unexpected Secret keys: $keys"
push
first=$(secret_sha)
out=$(setup_secret)
grep -q 'kept the existing secret ID' <<<"$out" && [ "$(secret_sha)" = "$first" ] || fail "second run changed the secret ID"
out=$(setup_secret --rotate-secret-id)
[ "$(secret_sha)" != "$first" ] || fail "rotation did not change the secret ID"
old=$(sed -n 's/.*secret_id_accessor=//p' <<<"$out")
[ -n "$old" ] || fail "rotation did not report the previous accessor"
push
# Revoke every other secret ID of the role: only the Secret's current one may still work.
current=$(on_host "kubectl get secret $CLUSTER-openbao-kms -n default -o jsonpath={.data.secretIDAccessor} | base64 -d")
for acc in $(as_root_bao list -format=json "auth/$APPROLE/role/$CLUSTER/secret-id" | jq -r '.[]'); do
  [ "$acc" = "$current" ] || as_root_bao write "auth/$APPROLE/role/$CLUSTER/secret-id-accessor/destroy" secret_id_accessor="$acc" >/dev/null
done
on_host 'systemctl restart vault-kubernetes-kms && kubectl create secret generic e2e-after-rotation -n default --from-literal=value=x >/dev/null'
[ "$(k3s_pid)" = "$before" ] || fail "k3s restarted during rotation"
pass "Secret has the documented keys; push-to-host installs it; rotated secret ID in use, older ones destroyed, k3s untouched"

step "install.sh refuses to enable KMS on a cluster that already has data"
on_host 'mv /etc/rancher/k3s/config.yaml.d/90-vault-kms.yaml /root/'
if on_host 'bash /opt/openbao-kms/install.sh < /root/kms-connection' >/dev/null 2>&1; then
  fail "install.sh accepted an existing cluster"
fi
on_host 'mv /root/90-vault-kms.yaml /etc/rancher/k3s/config.yaml.d/'
pass "refused"

printf '\nALL CHECKS PASSED\n'
