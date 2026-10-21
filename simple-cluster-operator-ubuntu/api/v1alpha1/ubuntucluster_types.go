/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FinalizerName = "ubuntucluster.apeirora.eu/finalizer"

	ConditionReady = "Ready"
	ConditionError = "Error"
	// ConditionEncryptionReady is set only when spec.encryption is requested:
	// True once every control-plane host runs the KMS plugin and its
	// kube-apiserver reports the kms-providers check healthy.
	ConditionEncryptionReady = "EncryptionReady"

	// SSHSecretPrivateKeyKey is the data key the operator reads to
	// authenticate against each host. Matches the
	// `kubernetes.io/ssh-auth` Secret convention so it interops with
	// what simple-cluster-operator-openstack writes.
	SSHSecretPrivateKeyKey = "ssh-privatekey"

	// SSHSecretPasswordKey is the data key for password login, matching
	// the `kubernetes.io/basic-auth` Secret convention.
	SSHSecretPasswordKey = "password"

	// KubeconfigSecretSuffix and KubeconfigSecretKey define where the
	// rendered cluster kubeconfig lands.
	KubeconfigSecretSuffix = "-kubeconfig"
	KubeconfigSecretKey    = "kubeconfig"

	RoleControlPlane = "control-plane"
	RoleWorker       = "worker"
)

// +kubebuilder:validation:Enum=Init;Provisioning;Ready;Failed;Deleting
type Phase string

const (
	// PhaseInit is the first phase set once the operator picks the CR
	// up. Covers credential reads and host probing — before any k3s
	// install runs.
	PhaseInit         Phase = "Init"
	PhaseProvisioning Phase = "Provisioning"
	PhaseReady        Phase = "Ready"
	PhaseFailed       Phase = "Failed"
	PhaseDeleting     Phase = "Deleting"
)

// UbuntuClusterSpec is the user request: a list of hosts already running
// Ubuntu plus the SSH key and k3s knobs.
// +kubebuilder:validation:XValidation:rule="has(self.encryption) == has(oldSelf.encryption)",message="encryption cannot be added or removed after creation"
// +kubebuilder:validation:XValidation:rule="!has(self.encryption) || !has(self.k3sServerArgs) || !self.k3sServerArgs.exists(a, a == '--secrets-encryption' || a.startsWith('--secrets-encryption='))",message="k3s --secrets-encryption cannot be combined with spec.encryption"
type UbuntuClusterSpec struct {
	// Version is the k3s release tag (e.g. "v1.31.4+k3s1"). Empty falls
	// back to the get.k3s.io installer's default (latest stable).
	// +optional
	Version string `json:"version,omitempty"`

	// ControlPlaneEndpoint is the public address (host or host:port)
	// that clients reach the kube-apiserver on. Must be backed by a
	// load balancer or VIP for true HA when more than one CP node is
	// declared. When empty, the first control-plane host's address is
	// used — fine for single-CP, not real HA.
	// +optional
	ControlPlaneEndpoint string `json:"controlPlaneEndpoint,omitempty"`

	// SSH is the credential + login used to reach every host.
	SSH SSHAuth `json:"ssh"`

	// ControlPlane is the set of hosts that will run `k3s server`
	// (embedded etcd, kube-apiserver, scheduler, controller-manager).
	// +kubebuilder:validation:MinProperties=1
	ControlPlane HostPool `json:"controlPlane"`

	// Workers is the set of hosts that will run `k3s agent`. May be
	// empty (single-node or all-CP cluster).
	// +optional
	Workers HostPool `json:"workers,omitempty,omitzero"`

	// K3sServerArgs are passed verbatim to `k3s server` (e.g.
	// `--disable=traefik`, `--cluster-cidr=10.42.0.0/16`).
	// +optional
	K3sServerArgs []string `json:"k3sServerArgs,omitempty"`

	// K3sAgentArgs are passed verbatim to `k3s agent` on every worker.
	// +optional
	K3sAgentArgs []string `json:"k3sAgentArgs,omitempty"`

	// Encryption opts the cluster into encryption at rest of its Kubernetes
	// Secrets with an external KMS provider, from the first write. Every
	// control-plane host gets the KMS v2 plugin before k3s is installed on
	// it; a host that already runs k3s without the plugin is refused, never
	// silently accepted. Immutable: it can be neither added, removed nor
	// changed after creation. Mirrors SimpleCluster.spec.encryption.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="encryption is immutable"
	Encryption *EncryptionSpec `json:"encryption,omitempty"`
}

// SSHAuth describes how the operator logs in. Exactly one of
// PrivateKeySecretRef (SSH key) or PasswordSecretRef (username/password)
// must be set; the chosen method applies to every host in the cluster.
// +kubebuilder:validation:XValidation:rule="has(self.privateKeySecretRef) != has(self.passwordSecretRef)",message="exactly one of privateKeySecretRef or passwordSecretRef must be set"
type SSHAuth struct {
	// User is the sudo-capable login the operator SSHs as
	// (e.g. "ubuntu", "eouser", "root").
	// +kubebuilder:validation:MinLength=1
	User string `json:"user"`

	// Port is the SSH port. Defaults to 22 when unset.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// PrivateKeySecretRef points at a Secret carrying the SSH private key
	// under the `ssh-privatekey` data key (the standard
	// `kubernetes.io/ssh-auth` shape). Namespace is optional and defaults
	// to the UbuntuCluster's own namespace when empty.
	// +optional
	PrivateKeySecretRef *corev1.SecretReference `json:"privateKeySecretRef,omitempty"`

	// PasswordSecretRef points at a Secret carrying the login password
	// under the `password` data key (the standard `kubernetes.io/basic-auth`
	// shape). Namespace is optional and defaults to the UbuntuCluster's own
	// namespace when empty.
	// +optional
	PasswordSecretRef *corev1.SecretReference `json:"passwordSecretRef,omitempty"`
}

// HostPool is just a wrapper to give group-level options room to grow
// without breaking the API.
type HostPool struct {
	// Hosts lists every host in this pool. Order is preserved and the
	// first host is the "bootstrap" control-plane (the one that runs
	// `k3s server --cluster-init`).
	// +listType=map
	// +listMapKey=name
	Hosts []Host `json:"hosts,omitempty"`
}

// Host is one Ubuntu server the operator will SSH into.
type Host struct {
	// Name uniquely identifies the host inside the cluster and is used
	// as the Nova/Linux hostname during k3s registration.
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Address is the host's internal IP/DNS name — the address other
	// cluster members use for k3s peer/agent traffic. Operator dials
	// here too unless ExternalAddress is set or the host sits behind
	// the bootstrap CP (bastion routing).
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address"`

	// ExternalAddress is the host's public IP/DNS name when one exists.
	// The bootstrap control-plane MUST have one — operator SSHes there
	// directly and uses it for the kubeconfig and TLS-SAN. Workers and
	// additional CPs typically omit it: operator reaches them via the
	// bootstrap CP as a ProxyJump bastion.
	// +optional
	ExternalAddress string `json:"externalAddress,omitempty"`

	// PasswordSecretRef overrides spec.ssh for THIS host with password
	// login: a `kubernetes.io/basic-auth` Secret read for the password
	// ("password" key) and, when present, the login ("username" key,
	// else spec.ssh.User). Use when each host has its own password — e.g.
	// per-server root passwords from a provider that can't inject SSH keys.
	// When unset the cluster-wide spec.ssh credential applies.
	// +optional
	PasswordSecretRef *corev1.SecretReference `json:"passwordSecretRef,omitempty"`
}

// UbuntuClusterStatus is the observed state.
type UbuntuClusterStatus struct {
	// Phase is the coarse state.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ControlPlaneEndpoint mirrors the address actually written into
	// the kubeconfig.
	// +optional
	ControlPlaneEndpoint string `json:"controlPlaneEndpoint,omitempty"`

	// K3sVersion is the version reported by `k3s --version` on the
	// bootstrap control-plane.
	// +optional
	K3sVersion string `json:"k3sVersion,omitempty"`

	// KubeconfigSecretRef references the Secret in this namespace
	// holding the cluster's kubeconfig under key "kubeconfig".
	// +optional
	KubeconfigSecretRef *corev1.LocalObjectReference `json:"kubeconfigSecretRef,omitempty"`

	// ReadyHosts and Hosts are counters for the printer columns:
	// ReadyHosts is how many hosts the operator has driven to
	// status=Ready; Hosts is the total declared.
	// +optional
	ReadyHosts int32 `json:"readyHosts,omitempty"`
	// +optional
	Hosts int32 `json:"hosts,omitempty"`

	// CP is "<readyControlPlanes>/<declaredControlPlanes>" — surfaced
	// as the CP printer column (e.g. "1/3").
	// +optional
	CP string `json:"cp,omitempty"`

	// WKR is "<readyWorkers>/<declaredWorkers>" — surfaced as the
	// WKR printer column (e.g. "0/2").
	// +optional
	WKR string `json:"wkr,omitempty"`

	// HostStatuses lists per-host install state, keyed by name.
	// +listType=map
	// +listMapKey=name
	// +optional
	HostStatuses []HostStatus `json:"hostStatuses,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Message is a one-line summary of Phase for consumers that read a
	// scalar (the SimpleCluster default readiness check does).
	// +optional
	Message string `json:"message,omitempty"`

	// Encryption reports the state of spec.encryption. Present only when
	// encryption is requested.
	// +optional
	Encryption *EncryptionStatus `json:"encryption,omitempty"`
}

// EncryptionSpec selects how the cluster's Secrets are encrypted in etcd.
// The shape follows Gardener's encryptionConfig.provider and
// SimpleCluster.spec.encryption.
type EncryptionSpec struct {
	// Provider is the key management provider the kube-apiserver uses through
	// a KMS v2 plugin. Only Secrets are encrypted.
	Provider EncryptionProvider `json:"provider"`
}

// EncryptionProviderType names an external key management provider.
// +kubebuilder:validation:Enum=OpenBao
type EncryptionProviderType string

const (
	// EncryptionProviderOpenBao encrypts with a per-cluster OpenBao Transit
	// key through the vault-kubernetes-kms plugin.
	EncryptionProviderOpenBao EncryptionProviderType = "OpenBao"
)

// EncryptionProvider is the key management provider and its key settings.
type EncryptionProvider struct {
	// Type of the provider.
	Type EncryptionProviderType `json:"type"`

	// KeyRotationPeriod rotates the cluster's key automatically at this
	// interval (at least 1h). Unset means no automatic rotation. Data written
	// under older key versions stays readable.
	// +optional
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1h')",message="keyRotationPeriod must be at least 1h"
	KeyRotationPeriod *metav1.Duration `json:"keyRotationPeriod,omitempty"`
}

// EncryptionEnabled reports whether the cluster asked for encryption at rest.
func (s *UbuntuClusterSpec) EncryptionEnabled() bool {
	return s.Encryption != nil
}

// EncryptionStatus is what the operator set up in the KMS provider for this
// cluster and how far each control-plane host got. It names objects, never
// credentials.
type EncryptionStatus struct {
	// Provider echoes spec.encryption.provider.type.
	Provider EncryptionProviderType `json:"provider"`
	// KeyName is the Transit key the cluster encrypts with (the cluster's
	// own name). The key is kept when the cluster is deleted so that etcd
	// snapshots stay restorable; removing it is an OpenBao admin action.
	// +optional
	KeyName string `json:"keyName,omitempty"`
	// KeyVersion is the latest key version the provider reported.
	// +optional
	KeyVersion int64 `json:"keyVersion,omitempty"`
	// RoleName is the AppRole role the hosts' plugins log in with.
	// +optional
	RoleName string `json:"roleName,omitempty"`
	// Hosts lists each control-plane host's plugin state.
	// +optional
	// +listType=map
	// +listMapKey=name
	Hosts []HostEncryptionStatus `json:"hosts,omitempty"`
	// CleanupDone is true once the role and the hosts' secret IDs were
	// removed from the provider during deletion. The key is kept.
	// +optional
	CleanupDone bool `json:"cleanupDone,omitempty"`
}

// HostEncryptionStatus is one control-plane host's KMS plugin state.
type HostEncryptionStatus struct {
	// Name is the host name from spec.
	Name string `json:"name"`
	// SecretIDAccessor identifies (without revealing) the AppRole secret ID
	// this host unwrapped. It is destroyed when the cluster is deleted or
	// when the secret ID is replaced.
	// +optional
	SecretIDAccessor string `json:"secretIDAccessor,omitempty"`
	// SecretIDIssuedAt is when the current secret ID was minted. The
	// operator replaces it before the role's secret ID TTL runs out.
	// +optional
	SecretIDIssuedAt *metav1.Time `json:"secretIDIssuedAt,omitempty"`
	// Verified is true once the host's kube-apiserver reported the
	// kms-providers check healthy with the plugin active.
	Verified bool `json:"verified"`
}

// HostStatus mirrors the install lifecycle of a single host.
type HostStatus struct {
	// Name matches Host.Name.
	Name string `json:"name"`
	// Role is "control-plane" or "worker".
	// +optional
	Role string `json:"role,omitempty"`
	// Address echoes Host.Address.
	// +optional
	Address string `json:"address,omitempty"`
	// Phase is one of "Pending", "Installing", "Ready", "Failed".
	// +optional
	Phase string `json:"phase,omitempty"`
	// Message carries the latest installer / SSH error or info line.
	// +optional
	Message string `json:"message,omitempty"`
	// KubeletVersion is `k3s --version`'s reported version on this host.
	// +optional
	KubeletVersion string `json:"kubeletVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="CP",type=string,JSONPath=`.status.cp`
// +kubebuilder:printcolumn:name="WKR",type=string,JSONPath=`.status.wkr`
// +kubebuilder:printcolumn:name="K3s",type=string,JSONPath=`.status.k3sVersion`
// +kubebuilder:printcolumn:name="Kubeconfig",type=string,JSONPath=`.status.kubeconfigSecretRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UbuntuCluster installs k3s on a fleet of already-running Ubuntu hosts
// reached over SSH, materialising a Kubernetes cluster plus its
// kubeconfig Secret. Tailored for the "have boxes, want k8s" path —
// CloudFerro VMs created by simple-cluster-operator-openstack are the
// canonical upstream producer.
type UbuntuCluster struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec UbuntuClusterSpec `json:"spec"`
	// +optional
	Status UbuntuClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type UbuntuClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UbuntuCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UbuntuCluster{}, &UbuntuClusterList{})
}
