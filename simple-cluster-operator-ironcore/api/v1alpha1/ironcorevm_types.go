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
	FinalizerName = "ironcorevm.apeirora.eu/finalizer"

	ConditionReady = "Ready"
	ConditionError = "Error"
	// ConditionCloudProfile reports whether the requested flavor (spec.cloudProfile
	// or spec.resources) resolved to an existing MachineClass.
	ConditionCloudProfile = "CloudProfile"

	SSHSecretPrivateKeyKey = "ssh-privatekey"
	SSHSecretPublicKeyKey  = "ssh-publickey"

	// SSHKeyAPIGroup / SSHKeyKind / SSHKeyVersion identify the SSHKey custom
	// resource that spec.sshKeyRef may point at instead of a Secret.
	SSHKeyAPIGroup = "sshkeys.apeirora.eu"
	SSHKeyKind     = "SSHKey"
	SSHKeyVersion  = "v1alpha1"

	CredentialsKubeconfigKey = "kubeconfig"
	IgnitionSecretKey        = "ignition.yaml"

	LabelVMNamespace = "ironcore.apeirora.eu/vm-namespace"
	LabelVMName      = "ironcore.apeirora.eu/vm-name"
)

// +kubebuilder:validation:Enum=Init;Provisioning;Ready;Failed;Deleting
type Phase string

const (
	PhaseInit         Phase = "Init"
	PhaseProvisioning Phase = "Provisioning"
	PhaseReady        Phase = "Ready"
	PhaseFailed       Phase = "Failed"
	PhaseDeleting     Phase = "Deleting"
)

// IronCoreVMSpec describes one IronCore Machine.
// +kubebuilder:validation:XValidation:rule="has(self.cloudProfile) != has(self.resources)",message="set exactly one of spec.cloudProfile or spec.resources"
type IronCoreVMSpec struct {
	// IronCoreKubeconfig references a Secret carrying a kubeconfig for the IronCore
	// cluster under key `kubeconfig`. Set `namespace` to read the Secret from a
	// different namespace; when empty it defaults to the VM's namespace.
	IronCoreKubeconfig corev1.SecretReference `json:"ironcoreKubeconfig"`

	// IronCoreNamespace is the namespace in the IronCore cluster where resources
	// are created. When empty it defaults to the namespace set in the credentials
	// kubeconfig context, else "default".
	// +optional
	IronCoreNamespace string `json:"ironcoreNamespace,omitempty"`

	// Network is the IronCore Network name. The operator ensures it exists.
	// +kubebuilder:validation:MinLength=1
	Network string `json:"network"`

	// IPPool, when set, is an existing ipam Prefix the NIC allocates its /32 from.
	// When empty, the operator creates a root Prefix named after Network using
	// NetworkCIDR and allocates from that.
	// +optional
	IPPool string `json:"ipPool,omitempty"`

	// NetworkCIDR is the CIDR of the root Prefix the operator creates for Network
	// when IPPool is unset.
	// +optional
	// +kubebuilder:default="10.0.0.0/24"
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	NetworkCIDR string `json:"networkCIDR,omitempty"`

	// CloudProfile selects an IronCore MachineClass by name directly
	// (e.g. "memory-intel1-2cpu"), the same name as the matching Gardener
	// CloudProfile machineType. Mutually exclusive with Resources.
	// +optional
	CloudProfile string `json:"cloudProfile,omitempty"`

	// Resources defines the CPU/memory floor; the operator picks the smallest
	// MachineClass that satisfies it. Mutually exclusive with CloudProfile.
	// +optional
	Resources *NodeResources `json:"resources,omitempty"`

	// Image is the OCI reference for the boot image
	// (e.g. ghcr.io/ironcore-dev/gardenlinux:latest).
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// PublicIP, when true, attaches a Public VirtualIP to the NetworkInterface.
	// +optional
	PublicIP bool `json:"publicIP,omitempty"`

	// MachinePool sets the MachinePoolRef on the Machine. Empty means the
	// IronCore scheduler picks a pool.
	// +optional
	MachinePool string `json:"machinePool,omitempty"`

	// SSHUser is the OS user the SSH key is injected for via ignition.
	// +optional
	// +kubebuilder:default="root"
	SSHUser string `json:"sshUser,omitempty"`

	// UserData is extra payload written as a file + oneshot systemd unit via ignition.
	// +optional
	UserData string `json:"userData,omitempty"`

	// SSHKeyRef selects the SSH key material for the VM. It accepts either an
	// SSHKey custom resource (apiGroup sshkeys.apeirora.eu, kind SSHKey) — whose
	// public key is read directly — or a kubernetes.io/ssh-auth Secret (core
	// apiGroup, kind Secret) whose ssh-publickey is used (looked up, or generated
	// and stored when absent). namespace is optional and defaults to the VM's
	// namespace. When unset, a per-VM Secret `<vm-name>-ssh` is generated.
	// +optional
	SSHKeyRef *corev1.TypedObjectReference `json:"sshKeyRef,omitempty"`
}

// NodeResources is the per-VM CPU/memory floor used for MachineClass selection.
type NodeResources struct {
	// CPU is the minimum CPU count (e.g. "2" or "1500m").
	CPU resource.Quantity `json:"cpu"`

	// Memory is the minimum RAM (e.g. "4Gi").
	Memory resource.Quantity `json:"memory"`

	// MachineClassPattern narrows selection to MachineClass names matching this
	// Go regex.
	// +optional
	MachineClassPattern string `json:"machineClassPattern,omitempty"`
}

// IronCoreVMStatus is the observed state of one IronCore Machine.
type IronCoreVMStatus struct {
	// Phase is the coarse state.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// MachineID is the provider-internal Machine ID.
	// +optional
	MachineID string `json:"machineID,omitempty"`

	// MachineState mirrors Machine.status.state.
	// +optional
	MachineState string `json:"machineState,omitempty"`

	// InternalAddress is the IP from NetworkInterface.status.ips[0].
	// +optional
	InternalAddress string `json:"internalAddress,omitempty"`

	// ExternalAddress is the VirtualIP from NetworkInterface.status.virtualIP.
	// +optional
	ExternalAddress string `json:"externalAddress,omitempty"`

	// SSHSecretRef references the Secret on the management cluster holding the
	// ed25519 keypair. Keys: ssh-privatekey, ssh-publickey.
	// +optional
	SSHSecretRef *corev1.LocalObjectReference `json:"sshSecretRef,omitempty"`

	// ResolvedMachineClass is the MachineClass the operator picked.
	// +optional
	ResolvedMachineClass ResolvedMachineClass `json:"resolvedMachineClass,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ResolvedMachineClass records the MachineClass the operator selected.
type ResolvedMachineClass struct {
	// Name is the MachineClass name.
	// +optional
	Name string `json:"name,omitempty"`

	// CPU is the MachineClass cpu capability as integer cores.
	// +optional
	CPU int32 `json:"cpu,omitempty"`

	// Memory is the MachineClass memory capability as a human-readable size.
	// +optional
	Memory string `json:"memory,omitempty"`

	// Description is a one-line summary (cpu / memory).
	// +optional
	Description string `json:"description,omitempty"`

	// Image mirrors spec.image.
	// +optional
	Image string `json:"image,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=icvm
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.status.resolvedMachineClass.name`
// +kubebuilder:printcolumn:name="Internal",type=string,JSONPath=`.status.internalAddress`
// +kubebuilder:printcolumn:name="External",type=string,JSONPath=`.status.externalAddress`
// +kubebuilder:printcolumn:name="SSH",type=string,JSONPath=`.status.sshSecretRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// IronCoreVM requests one IronCore Machine. The operator generates a per-VM
// ed25519 SSH keypair, injects it via ignition, and exposes the machine's
// addresses in status.
type IronCoreVM struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec IronCoreVMSpec `json:"spec"`
	// +optional
	Status IronCoreVMStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type IronCoreVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IronCoreVM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IronCoreVM{}, &IronCoreVMList{})
}
