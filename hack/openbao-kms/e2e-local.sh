#!/usr/bin/env bash
# End-to-end check of the OpenBao KMS integration on one simulated control-plane
# host. OpenBao runs in dev-TLS mode; an Ubuntu container with systemd plays the
# VM, where setup-openbao.sh, install.sh and the stock k3s installer run the way
# simple-cluster-operator-ubuntu runs them over SSH. It then exercises the day-2
# paths: secret ID rotation, wrapped single-use secret IDs, Transit key rotation,
# verify-host.sh and removal with crypto-shredding.
#
# Needs Docker with privileged containers and internet access (k3s installer,
# plugin and etcd releases, images). Removes everything it created unless KEEP=1.
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
PREFIX='k8s:enc:kms:v2:openbao:'
ROOT_TOKEN=$(od -An -tx1 -N24 /dev/urandom | tr -d ' \n')
WORK=$(mktemp -d)

cleanup() {
  rm -rf "$WORK"
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
with_bao() { # a host command with the dev root token's bao environment
  docker exec -i -e BAO_ADDR="https://$P-openbao:8200" -e BAO_CACERT=/opt/openbao-ca.pem \
    -e BAO_TOKEN="$ROOT_TOKEN" -e BAO_NAMESPACE="$NS" "$P-host" bash -euo pipefail -c "$1"
}
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
# Values go to a file before any grep: under pipefail an early grep match can
# SIGPIPE etcdctl and turn a found leak into a pass.
etcd_value() { etcd_get get "$1" --print-value-only > "$WORK/value"; }
encrypted() { etcd_value "$1"; [ "$(head -c ${#PREFIX} "$WORK/value")" = "$PREFIX" ]; }
wait_apiserver() {
  for _ in $(seq 1 120); do
    on_host 'kubectl get --raw=/readyz >/dev/null 2>&1' && return 0
    sleep 2
  done
  fail "kube-apiserver not ready after 240s"
}
k3s_pid() { on_host 'systemctl show -p MainPID --value k3s'; }
destroy_secret_ids_except() { # <accessor to keep>
  for acc in $(as_root_bao list -format=json "auth/$APPROLE/role/$CLUSTER/secret-id" | jq -r '.[]'); do
    [ "$acc" = "$1" ] || as_root_bao write "auth/$APPROLE/role/$CLUSTER/secret-id-accessor/destroy" secret_id_accessor="$acc" >/dev/null
  done
}
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
      systemd systemd-sysv dbus curl ca-certificates jq python3 iptables kmod sudo \
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
order=$(on_host 'systemctl show -p After -p Wants k3s')
grep -q vault-kubernetes-kms.service <<<"$order" || fail "k3s.service is not ordered after the plugin"
ready=$(on_host 'kubectl get --raw="/readyz?verbose"')
grep -q '\[+\]kms-providers ok' <<<"$ready" || fail "kms-providers not ok"
pass "$(on_host 'k3s --version | head -1'); k3s ordered after the plugin; kms-providers ok"

step "Secrets are KMS v2 ciphertext in etcd"
marker="e2e-plaintext-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')"
on_host "kubectl create secret generic e2e-secret -n default --from-literal=value=$marker" >/dev/null
encrypted /registry/secrets/default/e2e-secret || fail "e2e-secret is not KMS v2 ciphertext"
[ "$(grep -c -a -F -- "$marker" "$WORK/value" || true)" = 0 ] || fail "plaintext found in etcd"
[ "$(on_host 'kubectl get secret e2e-secret -n default -o jsonpath={.data.value} | base64 -d')" = "$marker" ] \
  || fail "API read-back mismatch"
total=0 enc=0
while read -r key; do
  [ -n "$key" ] || continue
  total=$((total + 1))
  if encrypted "$key"; then enc=$((enc + 1)); fi
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
docker exec -i "$P-host" bash -c 'cat > /usr/local/bin/secret-lines && chmod +x /usr/local/bin/secret-lines' <<'EOF'
#!/bin/bash
# The Secret's data as `key=<base64>` lines, the way push-to-host.sh streams it.
kubectl get secret "$1" -n default -o json | jq -r '.data | to_entries[] | "\(.key)=\(.value)"'
EOF
push() { on_host "PUSH_TO_HOST_SSH=local-ssh bash /opt/openbao-kms/push-to-host.sh --secret default/$CLUSTER-openbao-kms --host localhost" >/dev/null; }
secret_sha() { on_host "kubectl get secret $CLUSTER-openbao-kms -n default -o jsonpath={.data.secretID} | sha256sum"; }
setup_secret() { setup_with --secret "default/$CLUSTER-openbao-kms" "$@"; }
before=$(k3s_pid)
# Capture, then grep: piping into grep -q would SIGPIPE the next line of output
# and, under pipefail, report a created Secret as a failure.
out=$(setup_secret)
grep -q 'created Secret' <<<"$out" || fail "Secret not created"
keys=$(on_host "kubectl get secret $CLUSTER-openbao-kms -n default -o json | jq -r '.data | keys | join(\",\")'")
[ "$keys" = "address,approleMount,approleRole,caCert,namespace,roleID,secretID,secretIDAccessor,transitKey,transitMount" ] \
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
destroy_secret_ids_except "$(on_host "kubectl get secret $CLUSTER-openbao-kms -n default -o jsonpath={.data.secretIDAccessor} | base64 -d")"
on_host 'systemctl restart vault-kubernetes-kms && kubectl create secret generic e2e-after-rotation -n default --from-literal=value=x >/dev/null'
[ "$(k3s_pid)" = "$before" ] || fail "k3s restarted during rotation"
pass "Secret has the documented keys; push-to-host installs it; rotated secret ID in use, older ones destroyed, k3s untouched"

step "wrapped single-use secret IDs (setup --no-secret-id, push-to-host --wrap-ttl)"
before=$(k3s_pid)
out=$(setup_secret --no-secret-id)
grep -q 'stored no secret ID' <<<"$out" || fail "setup did not switch to wrapped delivery"
held=$(on_host "kubectl get secret $CLUSTER-openbao-kms -n default -o json | jq -r '.data | has(\"secretID\") or has(\"secretIDAccessor\")'")
[ "$held" = false ] || fail "the Secret still holds a secret ID"
out=$(with_bao "PUSH_TO_HOST_SSH=local-ssh bash /opt/openbao-kms/push-to-host.sh --secret default/$CLUSTER-openbao-kms --host localhost --wrap-ttl 2m")
acc=$(sed -n 's/.*its accessor is //p' <<<"$out")
[ -n "$acc" ] || fail "install.sh did not report an unwrapped secret ID"
destroy_secret_ids_except "$acc"
on_host 'systemctl restart vault-kubernetes-kms && kubectl create secret generic e2e-after-wrap -n default --from-literal=value=x >/dev/null'
[ "$(k3s_pid)" = "$before" ] || fail "k3s restarted during the switch"
pass "the Secret holds no secret ID; the host unwrapped its own; every other one destroyed"
tok=$(as_root_bao write -format=json -wrap-ttl=2m -f "auth/$APPROLE/role/$CLUSTER/secret-id" | jq -r '.wrap_info.token')
as_root_bao unwrap "$tok" >/dev/null # someone else unwraps it first
env_before=$(on_host 'sha256sum /etc/vault-kms/env')
if err=$(on_host "{ secret-lines $CLUSTER-openbao-kms; echo secretIDWrapped=$(printf '%s' "$tok" | base64 | tr -d '\n'); } | bash /opt/openbao-kms/install.sh" 2>&1); then
  fail "install.sh accepted a wrapping token that was already used"
fi
grep -q 'already used or has expired' <<<"$err" || fail "unexpected error for a used token: $(tail -1 <<<"$err")"
[ "$(on_host 'sha256sum /etc/vault-kms/env')" = "$env_before" ] || fail "a refused token changed the env file"
pass "a token someone else already unwrapped is refused, and nothing changes"

step "Transit key rotation, re-encryption, retiring the old version"
count_encrypt() { on_host 'journalctl --no-pager -u vault-kubernetes-kms | grep -c "v2 encryption request" || true'; }
n=$(count_encrypt)
as_root_bao write -f "$TRANSIT/keys/$CLUSTER/rotate" >/dev/null
for _ in $(seq 1 60); do [ "$(count_encrypt)" -gt "$n" ] && break; sleep 5; done
[ "$(count_encrypt)" -gt "$n" ] || fail "the kube-apiserver did not start using the new key version within 5 minutes"
for attempt in 1 2 3; do
  on_host 'kubectl get secrets -A -o json | kubectl replace -f - >/dev/null' && break
  [ "$attempt" -lt 3 ] || fail "could not re-write every Secret"
  sleep 5
done
as_root_bao write "$TRANSIT/keys/$CLUSTER/config" min_decryption_version=2 >/dev/null
on_host 'systemctl restart k3s'
wait_apiserver
on_host 'kubectl get secrets -A -o json >/dev/null' || fail "a Secret is still encrypted under the retired key version"
encrypted /registry/secrets/default/e2e-secret || fail "e2e-secret lost its KMS v2 prefix"
pass "new key version in use, every Secret re-written, version 1 retired, all Secrets readable after a restart"

step "verify-host.sh on the host"
if ! out=$(on_host 'bash /opt/openbao-kms/verify-host.sh --restart' 2>&1); then
  printf '%s\n' "$out"
  fail "verify-host.sh reported failures"
fi
pass "verify-host.sh: $(grep -c '^PASS' <<<"$out") checks passed, including a k3s restart"

step "install.sh refuses to enable KMS on a cluster that already has data"
on_host 'mv /etc/rancher/k3s/config.yaml.d/90-vault-kms.yaml /root/'
if on_host 'bash /opt/openbao-kms/install.sh < /root/kms-connection' >/dev/null 2>&1; then
  fail "install.sh accepted an existing cluster"
fi
on_host 'mv /root/90-vault-kms.yaml /etc/rancher/k3s/config.yaml.d/'
pass "refused"

step "remove-openbao.sh: revoke access, then crypto-shred"
remove() { with_bao "bash /opt/openbao-kms/remove-openbao.sh --cluster $CLUSTER --secret default/$CLUSTER-openbao-kms $*"; }
if remove --delete-key >/dev/null 2>&1; then fail "--delete-key without --confirm-key was accepted"; fi
remove --delete-key --confirm-key "$CLUSTER"
if as_root_bao read "auth/$APPROLE/role/$CLUSTER" >/dev/null 2>&1; then fail "role still exists"; fi
if as_root_bao policy read "simplecluster-kms-$CLUSTER" >/dev/null 2>&1; then fail "policy still exists"; fi
if as_root_bao read "$TRANSIT/keys/$CLUSTER" >/dev/null 2>&1; then fail "key still exists"; fi
if on_host "kubectl get secret $CLUSTER-openbao-kms -n default" >/dev/null 2>&1; then fail "Secret still exists"; fi
if on_host 'systemctl restart vault-kubernetes-kms' >/dev/null 2>&1; then fail "the plugin could still log in"; fi
pass "role, secret IDs, policy, Secret and key removed; the plugin can no longer log in"

printf '\nALL CHECKS PASSED\n'
