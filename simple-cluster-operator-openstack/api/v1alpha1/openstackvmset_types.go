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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FinalizerName = "openstackvmset.apeirora.eu/finalizer"

	ConditionReady = "Ready"
	ConditionError = "Error"

	// SSHSecretPrivateKeyKey is the data key under which the generated
	// ed25519 private key is stored in the SSH Secret.
	SSHSecretPrivateKeyKey = "ssh-privatekey"
	// SSHSecretPublicKeyKey is the data key under which the matching
	// public key is stored (OpenSSH single-line format).
	SSHSecretPublicKeyKey = "ssh-publickey"

	// CredentialsAuthURLKey is the data key holding the Keystone URL.
	CredentialsAuthURLKey = "OS_AUTH_URL"
	// CredentialsRegionKey is the data key holding the OpenStack region.
	CredentialsRegionKey = "OS_REGION_NAME"
	// CredentialsAppCredIDKey is the data key holding the application
	// credential ID.
	CredentialsAppCredIDKey = "OS_APPLICATION_CREDENTIAL_ID"
	// CredentialsAppCredSecretKey is the data key holding the application
	// credential secret.
	CredentialsAppCredSecretKey = "OS_APPLICATION_CREDENTIAL_SECRET"
)

// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Failed;Deleting
type Phase string

const (
	PhasePending      Phase = "Pending"
	PhaseProvisioning Phase = "Provisioning"
	PhaseReady        Phase = "Ready"
	PhaseFailed       Phase = "Failed"
	PhaseDeleting     Phase = "Deleting"
)

// OpenStackVMSetSpec describes a set of Nova servers to keep running.
type OpenStackVMSetSpec struct {
	// CredentialsRef is a Secret in the same namespace carrying Keystone
	// v3 application credentials. Required keys:
	// OS_AUTH_URL, OS_REGION_NAME, OS_APPLICATION_CREDENTIAL_ID,
	// OS_APPLICATION_CREDENTIAL_SECRET.
	CredentialsRef corev1.LocalObjectReference `json:"credentialsRef"`

	// Network is the name (or UUID) of the Neutron network each server
	// gets a port on.
	// +kubebuilder:validation:MinLength=1
	Network string `json:"network"`

	// FloatingIPPool is the name (or UUID) of the external network from
	// which floating IPs are allocated for groups that opt in.
	// +optional
	FloatingIPPool string `json:"floatingIPPool,omitempty"`

	// SecurityGroups is the list of Neutron security-group names attached
	// to every port. If empty the project default is used.
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// Groups is the list of node groups (e.g. control-plane, workers).
	// The operator ensures each group's `count` is reached.
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	Groups []VMGroup `json:"groups"`
}

// VMGroup is one homogeneous pool of Nova servers.
type VMGroup struct {
	// Name uniquely identifies the group inside this VMSet. Used as a
	// label on every server and as a key in status.groups.
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	// +kubebuilder:validation:MaxLength=30
	Name string `json:"name"`

	// Count is how many servers this group should have running.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=50
	Count int32 `json:"count"`

	// Resources is the per-VM CPU/memory floor. The operator picks the
	// smallest Nova flavor that meets BOTH requirements and records the
	// choice in status.groups[].flavor.
	Resources NodeResources `json:"resources"`

	// Image is the Glance image name (e.g. "Ubuntu 24.04 LTS").
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// FloatingIP when true allocates one FIP from spec.floatingIPPool
	// per server in this group.
	// +optional
	FloatingIP bool `json:"floatingIP,omitempty"`

	// UserData is cloud-init payload injected verbatim (already plain
	// text — the operator base64-encodes it before calling Nova).
	// +optional
	UserData string `json:"userData,omitempty"`
}

// NodeResources is the per-VM CPU/memory request. The operator picks
// the smallest visible Nova flavor whose vCPUs >= cpu and RAM >= memory.
type NodeResources struct {
	// CPU is the minimum vCPU count. Parsed as resource.Quantity so both
	// integer ("2") and millicore ("1500m") forms work; millicores are
	// rounded up since Nova flavors are integer-vCPU.
	CPU resource.Quantity `json:"cpu"`

	// Memory is the minimum RAM (e.g. "4Gi", "32GB"). Rounded up to MiB
	// to compare with Nova's flavor RAM field.
	Memory resource.Quantity `json:"memory"`

	// FlavorPattern, when set, narrows the picker to Nova flavors whose
	// name matches this Go regex. Use it to exclude specialised
	// hardware: e.g. `^eo[12]a\.` keeps only CloudFerro general-purpose
	// flavors and rules out gpu.* / vm.l40s.* which otherwise win the
	// "smallest fit" comparison.
	// +optional
	FlavorPattern string `json:"flavorPattern,omitempty"`
}

// OpenStackVMSetStatus is the observed state of an OpenStackVMSet.
type OpenStackVMSetStatus struct {
	// Phase is the coarse state.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// SSHSecretRef references the Secret in this namespace holding the
	// ed25519 keypair the operator generated and uploaded as the
	// keypair-name `<vmset-name>`. Key names: ssh-privatekey, ssh-publickey.
	// +optional
	SSHSecretRef *corev1.LocalObjectReference `json:"sshSecretRef,omitempty"`

	// KeypairName is the name registered in Nova (equals the CR name).
	// +optional
	KeypairName string `json:"keypairName,omitempty"`

	// Groups mirrors per-group server inventory.
	// +listType=map
	// +listMapKey=name
	// +optional
	Groups []GroupStatus `json:"groups,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GroupStatus is the observed state of one VMGroup.
type GroupStatus struct {
	// Name matches VMGroup.Name.
	Name string `json:"name"`

	// Ready counts servers in ACTIVE state.
	// +optional
	Ready int32 `json:"ready,omitempty"`

	// Flavor is the Nova flavor name picked by the operator to satisfy
	// VMGroup.Resources (e.g. "eo1a.medium").
	// +optional
	Flavor string `json:"flavor,omitempty"`

	// FlavorDescription is a one-line summary of the picked flavor's
	// specs (vCPU / RAM / disk).
	// +optional
	FlavorDescription string `json:"flavorDescription,omitempty"`

	// Hosts lists every server's address summary.
	// +listType=map
	// +listMapKey=name
	// +optional
	Hosts []HostStatus `json:"hosts,omitempty"`
}

// HostStatus describes one Nova server visible to consumers (typically
// the ubuntu-cluster-operator).
type HostStatus struct {
	// Name is the Nova server name.
	Name string `json:"name"`
	// ID is the Nova server UUID.
	// +optional
	ID string `json:"id,omitempty"`
	// InternalIP is the first fixed IP on spec.network.
	// +optional
	InternalIP string `json:"internalIP,omitempty"`
	// FloatingIP is the allocated floating address, if any.
	// +optional
	FloatingIP string `json:"floatingIP,omitempty"`
	// Status mirrors Nova's `status` field (e.g. "ACTIVE", "BUILD", "ERROR").
	// +optional
	Status string `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=osvms
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="SSH",type=string,JSONPath=`.status.sshSecretRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OpenStackVMSet requests a set of Nova servers, organised in groups,
// on the OpenStack cloud whose credentials live in CredentialsRef.
type OpenStackVMSet struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec OpenStackVMSetSpec `json:"spec"`
	// +optional
	Status OpenStackVMSetStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type OpenStackVMSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenStackVMSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenStackVMSet{}, &OpenStackVMSetList{})
}
