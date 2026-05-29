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
	FinalizerName = "openstackvm.apeirora.eu/finalizer"

	ConditionReady = "Ready"
	ConditionError = "Error"

	// SSHSecretPrivateKeyKey is the data key under which the generated
	// ed25519 private key is stored in the per-VM SSH Secret.
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

// +kubebuilder:validation:Enum=Init;Provisioning;Ready;Failed;Deleting
type Phase string

const (
	// PhaseInit is the first phase set when the operator first picks up
	// a CR. It covers credential / SSH-keypair / network resolution —
	// i.e. all the pre-VM setup. Transitions to Provisioning once the
	// operator starts creating or deleting the Nova server.
	PhaseInit         Phase = "Init"
	PhaseProvisioning Phase = "Provisioning"
	PhaseReady        Phase = "Ready"
	PhaseFailed       Phase = "Failed"
	PhaseDeleting     Phase = "Deleting"
)

// OpenStackVMSpec describes one Nova server. The CR is self-contained:
// every spec field is what Nova actually needs (no shared parent CR).
type OpenStackVMSpec struct {
	// CredentialsRef is a Secret in the same namespace carrying Keystone
	// v3 application credentials. Required keys:
	// OS_AUTH_URL, OS_REGION_NAME, OS_APPLICATION_CREDENTIAL_ID,
	// OS_APPLICATION_CREDENTIAL_SECRET.
	CredentialsRef corev1.LocalObjectReference `json:"credentialsRef"`

	// Network is the name (or UUID) of the Neutron network this server's
	// port lives on.
	// +kubebuilder:validation:MinLength=1
	Network string `json:"network"`

	// FloatingIPPool is the name (or UUID) of the external network from
	// which a floating IP is allocated when FloatingIP is true.
	// +optional
	FloatingIPPool string `json:"floatingIPPool,omitempty"`

	// SecurityGroups is the list of Neutron security-group names attached
	// to the server's port. If empty the project default is used.
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// Resources is the per-VM CPU/memory floor. The operator picks the
	// smallest Nova flavor that meets both requirements and records the
	// choice in status.resolvedFlavor.
	Resources NodeResources `json:"resources"`

	// Image is the Glance image name (e.g. "Ubuntu 24.04 LTS").
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// FloatingIP, when true, allocates one FIP from spec.floatingIPPool
	// and attaches it to the server's port.
	// +optional
	FloatingIP bool `json:"floatingIP,omitempty"`

	// UserData is cloud-init payload injected verbatim (already plain
	// text — the operator base64-encodes it before calling Nova).
	// +optional
	UserData string `json:"userData,omitempty"`
}

// NodeResources is the per-VM CPU/memory request. The operator picks the
// smallest visible Nova flavor whose vCPUs >= cpu and RAM >= memory.
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

// OpenStackVMStatus is the observed state of one Nova server.
type OpenStackVMStatus struct {
	// Phase is the coarse state.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ServerID is the Nova server UUID once Nova accepts the create call.
	// +optional
	ServerID string `json:"serverID,omitempty"`

	// NovaStatus mirrors Nova's `status` field (e.g. "ACTIVE", "BUILD",
	// "ERROR"). Empty until the server exists.
	// +optional
	NovaStatus string `json:"novaStatus,omitempty"`

	// InternalAddress is the first fixed IP on spec.network.
	// +optional
	InternalAddress string `json:"internalAddress,omitempty"`

	// ExternalAddress is the allocated floating address, if any.
	// +optional
	ExternalAddress string `json:"externalAddress,omitempty"`

	// SSHSecretRef references the Secret in this namespace holding the
	// ed25519 keypair the operator generated for this VM. Key names:
	// ssh-privatekey, ssh-publickey.
	// +optional
	SSHSecretRef *corev1.LocalObjectReference `json:"sshSecretRef,omitempty"`

	// KeypairName is the name registered in Nova (equals the CR name).
	// +optional
	KeypairName string `json:"keypairName,omitempty"`

	// ResolvedFlavor is the operator's plan: the Nova flavor it picked
	// to satisfy Resources, plus a one-line summary.
	// +optional
	ResolvedFlavor ResolvedFlavor `json:"resolvedFlavor,omitempty,omitzero"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ResolvedFlavor is the operator's flavor pick + a small summary.
type ResolvedFlavor struct {
	// Flavor is the Nova flavor name picked (e.g. "eo1a.medium").
	// +optional
	Flavor string `json:"flavor,omitempty"`

	// FlavorDescription is a one-line summary of the picked flavor's
	// specs (vCPU / RAM / disk).
	// +optional
	FlavorDescription string `json:"flavorDescription,omitempty"`

	// CPU is the picked flavor's vCPU count.
	// +optional
	CPU int32 `json:"cpu,omitempty"`

	// Memory is the picked flavor's RAM as a human-readable size
	// (e.g. "4 GiB", "512 MiB").
	// +optional
	Memory string `json:"memory,omitempty"`

	// Image mirrors spec.Image — the Glance image name handed to Nova.
	// +optional
	Image string `json:"image,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=osvm
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Flavor",type=string,JSONPath=`.status.resolvedFlavor.flavor`
// +kubebuilder:printcolumn:name="Internal",type=string,JSONPath=`.status.internalAddress`
// +kubebuilder:printcolumn:name="External",type=string,JSONPath=`.status.externalAddress`
// +kubebuilder:printcolumn:name="SSH",type=string,JSONPath=`.status.sshSecretRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OpenStackVM requests one Nova server on the OpenStack cloud whose
// credentials live in CredentialsRef. The operator generates a per-VM
// ed25519 SSH keypair and publishes both the public key to Nova and the
// keypair to a Secret named `<vm-name>-ssh`.
type OpenStackVM struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec OpenStackVMSpec `json:"spec"`
	// +optional
	Status OpenStackVMStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type OpenStackVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenStackVM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenStackVM{}, &OpenStackVMList{})
}
