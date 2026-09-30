#!/usr/bin/env bash
# Installs the vault-kubernetes-kms KMS v2 plugin on a k3s control-plane host,
# ahead of k3s, so the kube-apiserver encrypts Secrets with an OpenBao Transit key
# from its first write. See docs/openbao-kms.md.
#
# Run as root on the host BEFORE the k3s installer. The connection Secret's data
# arrives on stdin as `key=<base64 value>` lines (push-to-host.sh produces them);
# credentials never appear in arguments, unit files or k3s configuration. The
# secret ID comes either as `secretID` or as `secretIDWrapped`, a single-use
# response-wrapping token that this script unwraps on the host.
#
# What it lays down:
#   /usr/local/bin/vault-kubernetes-kms                  pinned release, sha256-verified
#   /etc/vault-kms/env                                   0600 root; systemd EnvironmentFile
#   /etc/vault-kms/ca.pem                                only when the Secret carries caCert
#   /etc/vault-kms/encryption-config.yaml                KMS v2 for Secrets, no plaintext fallback
#   /etc/systemd/system/vault-kubernetes-kms.service     starts before k3s.service
#   /etc/systemd/system/k3s.service.d/10-vault-kms.conf  orders k3s after the plugin
#   /etc/rancher/k3s/config.yaml.d/90-vault-kms.yaml     points kube-apiserver at the config
#
# Re-running with new data (e.g. a rotated secret ID) updates the env file and
# restarts the plugin only; k3s keeps running.
set -euo pipefail

KMS_VERSION=1.4.0
declare -A KMS_SHA256=(
  [x86_64]=1fd3a895d3e9af4228ea6ccb6b106d21c46e0860335043936e06013e7a06298c
  [arm64]=1d23efafe6c57cceb29d79acd9611db81715b0767086dc2b9e4ba5332bb9e2a0
)
# Tests may pre-stage the release tarball; it is still checksum-verified.
KMS_TARBALL=${KMS_TARBALL:-}

CONF_DIR=/etc/vault-kms
BIN=/usr/local/bin/vault-kubernetes-kms
UNIT=/etc/systemd/system/vault-kubernetes-kms.service
DROPIN=/etc/systemd/system/k3s.service.d/10-vault-kms.conf
K3S_SNIPPET=/etc/rancher/k3s/config.yaml.d/90-vault-kms.yaml
SOCKET=/run/vault-kms/vaultkms.socket
# Upstream binds its health/metrics server on all interfaces and has no address
# flag. 8080 would clash with k3s ServiceLB host ports; firewall this port.
HEALTH_PORT=10271

die() { echo "install.sh: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root"
command -v systemctl >/dev/null || die "systemd is required"

# --------------------------------------------------------------- read input
declare -A in=()
while IFS= read -r line || [ -n "$line" ]; do
  [ -z "$line" ] && continue
  key=${line%%=*}
  val=${line#*=}
  case $key in
    address|namespace|transitMount|transitKey|approleMount|approleRole|roleID|secretID|secretIDAccessor|secretIDWrapped|caCert) ;;
    *) die "unexpected key in input: $key" ;;
  esac
  in[$key]=$(printf '%s' "$val" | base64 -d) || die "value of $key is not base64"
done

for k in address transitMount transitKey approleMount roleID; do
  [ -n "${in[$k]:-}" ] || die "missing required key: $k"
done
[ -n "${in[secretID]:-}" ] || [ -n "${in[secretIDWrapped]:-}" ] || die "missing required key: secretID or secretIDWrapped"
case ${in[address]} in https://*) ;; *) die "address must be https:// (TLS is always verified)" ;; esac
for k in address namespace transitMount transitKey approleMount roleID secretID secretIDWrapped; do
  case ${in[$k]:-} in *[[:space:]\"\'\\]*) die "value of $k contains whitespace, quotes or backslashes" ;; esac
done

# ------------------------------------------------ refuse to encrypt a live cluster
# Adding KMS to a cluster that already stored Secrets would make them unreadable
# (the config has no plaintext fallback). Migration is out of scope.
if [ -d /var/lib/rancher/k3s/server/db ] && [ ! -f "$K3S_SNIPPET" ]; then
  die "k3s already has data on this host without the KMS config; refusing to enable KMS on an existing cluster"
fi

# ------------------------------------------------------------------- binary
arch=$(uname -m)
case $arch in x86_64) ;; aarch64|arm64) arch=arm64 ;; *) die "unsupported architecture $arch" ;; esac
if [ -x "$BIN" ] && "$BIN" -version 2>/dev/null | grep -qx "vault-kubernetes-kms v$KMS_VERSION"; then
  echo "vault-kubernetes-kms v$KMS_VERSION already installed"
else
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  tgz="vault-kubernetes-kms_Linux_${arch}.tar.gz"
  if [ -n "$KMS_TARBALL" ]; then
    cp "$KMS_TARBALL" "$tmp/$tgz"
  else
    curl -fsSL -o "$tmp/$tgz" \
      "https://github.com/FalcoSuessgott/vault-kubernetes-kms/releases/download/v$KMS_VERSION/$tgz"
  fi
  echo "${KMS_SHA256[$arch]}  $tmp/$tgz" | sha256sum -c --quiet - || die "checksum mismatch for $tgz"
  tar -xzf "$tmp/$tgz" -C "$tmp" vault-kubernetes-kms
  install -m 0755 "$tmp/vault-kubernetes-kms" "$BIN"
  echo "installed vault-kubernetes-kms v$KMS_VERSION"
fi

# -------------------------------------------------------------- configuration
install -d -m 0755 "$CONF_DIR"
changed=false

# write_if_changed <path> <mode>: content on stdin; sets changed=true on change.
write_if_changed() {
  local path=$1 mode=$2 new
  new=$(mktemp "$CONF_DIR/.new.XXXXXX")
  cat > "$new"
  chmod "$mode" "$new"
  if [ -f "$path" ] && cmp -s "$new" "$path"; then
    rm -f "$new"
  else
    mv -f "$new" "$path"
    changed=true
  fi
}

ca_line=
if [ -n "${in[caCert]:-}" ]; then
  write_if_changed "$CONF_DIR/ca.pem" 0644 < <(printf '%s\n' "${in[caCert]}")
  ca_line="VAULT_KMS_VAULT_CACERT=$CONF_DIR/ca.pem"
fi

# A wrapped secret ID is single-use and short-lived, so it is unwrapped only now,
# after everything that could fail first. A token that no longer unwraps was used
# by someone else or expired; the first case means the secret ID is compromised.
if [ -n "${in[secretIDWrapped]:-}" ]; then
  command -v python3 >/dev/null || die "python3 is required to unwrap a secret ID"
  hdrs="X-Vault-Token: ${in[secretIDWrapped]}"
  [ -z "${in[namespace]:-}" ] || hdrs+=$'\n'"X-Vault-Namespace: ${in[namespace]}"
  curl_ca=()
  [ -z "${in[caCert]:-}" ] || curl_ca=(--cacert "$CONF_DIR/ca.pem")
  resp=$(printf '%s\n' "$hdrs" | curl -sS -X POST -H @- ${curl_ca[@]+"${curl_ca[@]}"} \
    --write-out '\n%{http_code}' "${in[address]}/v1/sys/wrapping/unwrap") || die "cannot reach OpenBao to unwrap the secret ID"
  unset hdrs 'in[secretIDWrapped]'
  code=${resp##*$'\n'}
  if [ "$code" != 200 ]; then
    why=$(printf '%s' "${resp%$'\n'*}" | python3 -c 'import json,sys
try: print("; ".join(json.load(sys.stdin).get("errors", [])))
except Exception: print("unreadable response")')
    die "could not unwrap the secret ID (HTTP $code: $why). It was already used or has expired; if nobody here used it, treat it as compromised and destroy the role's secret IDs."
  fi
  unwrapped=$(printf '%s' "${resp%$'\n'*}" | python3 -c 'import json,sys
d = json.load(sys.stdin)["data"]; print(d["secret_id"]); print(d["secret_id_accessor"])')
  unset resp
  in[secretID]=${unwrapped%%$'\n'*}
  echo "unwrapped a single-use secret ID; its accessor is ${unwrapped##*$'\n'}"
  unset unwrapped
  case ${in[secretID]} in ""|*[[:space:]\"\'\\]*) die "unwrapped secret ID is malformed" ;; esac
fi

# mktemp creates the file 0600 before any content is written.
write_if_changed "$CONF_DIR/env" 0600 < <(
  echo "VAULT_KMS_VAULT_ADDR=${in[address]}"
  [ -n "${in[namespace]:-}" ] && echo "VAULT_KMS_VAULT_NAMESPACE=${in[namespace]}"
  [ -n "$ca_line" ] && echo "$ca_line"
  echo "VAULT_KMS_AUTH_METHOD=approle"
  echo "VAULT_KMS_APPROLE_MOUNT=${in[approleMount]}"
  echo "VAULT_KMS_APPROLE_ROLE_ID=${in[roleID]}"
  echo "VAULT_KMS_APPROLE_SECRET_ID=${in[secretID]}"
  echo "VAULT_KMS_TRANSIT_MOUNT=${in[transitMount]}"
  echo "VAULT_KMS_TRANSIT_KEY=${in[transitKey]}"
)
unset 'in[secretID]'

write_if_changed "$CONF_DIR/encryption-config.yaml" 0600 <<EOF
# Managed by vault-kubernetes-kms install.sh. KMS v2 only, no identity provider:
# a KMS failure blocks Secret writes instead of storing plaintext.
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - kms:
          apiVersion: v2
          name: openbao
          endpoint: unix://$SOCKET
          timeout: 3s
EOF

write_if_changed "$UNIT" 0644 <<EOF
# Managed by vault-kubernetes-kms install.sh.
[Unit]
Description=Kubernetes KMS v2 plugin backed by OpenBao Transit (vault-kubernetes-kms)
Documentation=https://github.com/apeirora/showroom-msp-simple-cluster/blob/main/docs/openbao-kms.md
Wants=network-online.target
After=network-online.target
Before=k3s.service
# Keep retrying while OpenBao is unreachable; never give up after a burst.
StartLimitIntervalSec=0

[Service]
Type=simple
Environment=VAULT_KMS_SOCKET=unix://$SOCKET
Environment=VAULT_KMS_FORCE_SOCKET_OVERWRITE=true
Environment=VAULT_KMS_DISABLE_V1=true
Environment=VAULT_KMS_HEALTH_PORT=$HEALTH_PORT
EnvironmentFile=$CONF_DIR/env
ExecStart=$BIN
# The socket appears only after the first OpenBao login succeeded.
ExecStartPost=/bin/sh -c 'for i in \$\$(seq 1 150); do [ -S $SOCKET ] && exit 0; sleep 0.2; done; exit 1'
Restart=always
RestartSec=2
DynamicUser=yes
RuntimeDirectory=vault-kms
RuntimeDirectoryMode=0700
RuntimeDirectoryPreserve=restart
NoNewPrivileges=yes
CapabilityBoundingSet=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=yes
LockPersonality=yes
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
EOF

# Wants, not Requires: restarting the plugin (secret rotation) must not restart k3s.
install -d -m 0755 "$(dirname "$DROPIN")"
write_if_changed "$DROPIN" 0644 <<EOF
# Managed by vault-kubernetes-kms install.sh.
[Unit]
Wants=vault-kubernetes-kms.service
After=vault-kubernetes-kms.service
EOF

install -d -m 0755 "$(dirname "$K3S_SNIPPET")"
write_if_changed "$K3S_SNIPPET" 0644 <<EOF
# Managed by vault-kubernetes-kms install.sh.
kube-apiserver-arg+:
  - encryption-provider-config=$CONF_DIR/encryption-config.yaml
EOF

# ------------------------------------------------------------------- start
systemctl daemon-reload
systemctl enable vault-kubernetes-kms.service >/dev/null 2>&1
if $changed; then
  systemctl restart vault-kubernetes-kms.service || true
else
  systemctl start vault-kubernetes-kms.service || true
fi

if ! systemctl is-active --quiet vault-kubernetes-kms.service || ! [ -S "$SOCKET" ]; then
  echo "install.sh: vault-kubernetes-kms is not running; last log lines:" >&2
  journalctl -u vault-kubernetes-kms.service -n 20 --no-pager >&2 || true
  exit 1
fi
curl -fsS "http://127.0.0.1:$HEALTH_PORT/health" >/dev/null || die "plugin health check failed"
echo "vault-kubernetes-kms is running; socket $SOCKET"
