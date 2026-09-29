# Least-privilege policy for one cluster's KMS plugin, rendered by
# setup-openbao.sh. Token lookup-self and renew-self come from the default policy.

# Wrap and unwrap the kube-apiserver's DEK seeds.
path "@TRANSIT_MOUNT@/encrypt/@TRANSIT_KEY@" {
  capabilities = ["update"]
}

path "@TRANSIT_MOUNT@/decrypt/@TRANSIT_KEY@" {
  capabilities = ["update"]
}

# Key metadata: the plugin reports the latest key version as the KMS v2 key ID,
# which is how the kube-apiserver notices a rotation.
path "@TRANSIT_MOUNT@/keys/@TRANSIT_KEY@" {
  capabilities = ["read"]
}
