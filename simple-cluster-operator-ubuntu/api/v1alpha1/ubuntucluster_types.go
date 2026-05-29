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

	// SSHSecretPrivateKeyKey is the data key the operator reads to
	// authenticate against each host. Matches the
	// `kubernetes.io/ssh-auth` Secret convention so it interops with
	// what simple-cluster-operator-openstack writes.
	SSHSecretPrivateKeyKey = "ssh-privatekey"

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
}

// SSHAuth describes how the operator logs in.
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

	// PrivateKeySecretRef points at a Secret in the same namespace
	// carrying the SSH private key under the `ssh-privatekey` data
	// key (the standard `kubernetes.io/ssh-auth` shape).
	PrivateKeySecretRef corev1.LocalObjectReference `json:"privateKeySecretRef"`
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

	// CPWorkers is "<controlPlanes>/<workers>" — declared counts from
	// the spec, surfaced as the CP_WKR printer column.
	// +optional
	CPWorkers string `json:"cpWorkers,omitempty"`

	// ReadyCPWorkers is "<readyControlPlanes>/<readyWorkers>" — counts
	// of hosts the operator has driven to Ready, surfaced as the
	// READY_CP_WKR printer column.
	// +optional
	ReadyCPWorkers string `json:"readyCPWorkers,omitempty"`

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
// +kubebuilder:printcolumn:name="CP_WKR",type=string,JSONPath=`.status.cpWorkers`
// +kubebuilder:printcolumn:name="READY_CP_WKR",type=string,JSONPath=`.status.readyCPWorkers`
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
