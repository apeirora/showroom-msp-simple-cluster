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
	// FinalizerName is set on UbuntuMachine so we can clear boundClaimRef
	// + return phase to Available if the object is deleted mid-bind.
	FinalizerName = "ubuntumachines.apeirora.eu/finalizer"

	// SSHSecretPrivateKeyKey matches the `kubernetes.io/ssh-auth` Secret
	// convention so a single key Secret interops with downstream
	// consumers (e.g. simple-cluster-operator-ubuntu).
	SSHSecretPrivateKeyKey = "ssh-privatekey"
)

// +kubebuilder:validation:Enum=Available;Bound
type MachinePhase string

const (
	// MachineAvailable means no claim currently owns this machine.
	MachineAvailable MachinePhase = "Available"
	// MachineBound means status.boundClaimRef points at a claim.
	MachineBound MachinePhase = "Bound"
)

// UbuntuMachineSpec describes a pre-existing Ubuntu host an administrator
// has registered into the pool. The operator does not provision or modify
// the underlying machine — it only tracks ownership.
type UbuntuMachineSpec struct {
	// Address is the host's internal IP/DNS name. Required.
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address"`

	// ExternalAddress is the host's public IP/DNS name when one exists.
	// Required for machines that consumers want to reach directly
	// (claims with `externalAddressRequired: true` will only bind these).
	// +optional
	ExternalAddress string `json:"externalAddress,omitempty"`

	// SSH is the credential the consumer is told to use. The operator
	// itself never opens an SSH connection — it just hands these refs
	// back through claim status.
	SSH SSHAuth `json:"ssh"`

	// Capacity advertises the resources this machine can satisfy. Used
	// by the claim binder: a claim's `required.cpu/memory` must be
	// `<=` capacity for the machine to be eligible. Zero (or omitted)
	// values mean "unconstrained" and match any request.
	// +optional
	Capacity NodeResources `json:"capacity,omitempty"`
}

// SSHAuth describes how a consumer should log in.
type SSHAuth struct {
	// User is the sudo-capable login (e.g. "ubuntu", "eouser", "root").
	// +kubebuilder:validation:MinLength=1
	User string `json:"user"`

	// Port is the SSH port. Defaults to 22 when unset.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// PrivateKeySecretRef points at a Secret in the same namespace as
	// the UbuntuMachine carrying the SSH private key under the
	// `ssh-privatekey` data key.
	PrivateKeySecretRef corev1.LocalObjectReference `json:"privateKeySecretRef"`
}

// NodeResources is the per-machine resource ad — mirrors the shape used
// by SimpleCluster.spec.{controlPlane,workers}.requested.
type NodeResources struct {
	// CPU is the advertised CPU (e.g. "2", "500m").
	// +optional
	CPU resource.Quantity `json:"cpu,omitempty"`
	// Memory is the advertised memory (e.g. "2Gi").
	// +optional
	Memory resource.Quantity `json:"memory,omitempty"`
}

// UbuntuMachineStatus is the observed state.
type UbuntuMachineStatus struct {
	// Phase is Available when no claim owns this machine, Bound when one
	// does.
	// +optional
	Phase MachinePhase `json:"phase,omitempty"`

	// BoundClaimRef points at the UbuntuMachineClaim that holds this
	// machine. Cleared when the claim is deleted.
	// +optional
	BoundClaimRef *corev1.LocalObjectReference `json:"boundClaimRef,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=um
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.spec.address`
// +kubebuilder:printcolumn:name="External",type=string,JSONPath=`.spec.externalAddress`
// +kubebuilder:printcolumn:name="CPU",type=string,JSONPath=`.spec.capacity.cpu`
// +kubebuilder:printcolumn:name="Memory",type=string,JSONPath=`.spec.capacity.memory`
// +kubebuilder:printcolumn:name="Claim",type=string,JSONPath=`.status.boundClaimRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UbuntuMachine is one pre-existing host an administrator registers into
// the pool. Consumers ask for machines via UbuntuMachineClaim — the
// operator never touches the host itself.
type UbuntuMachine struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec UbuntuMachineSpec `json:"spec"`
	// +optional
	Status UbuntuMachineStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type UbuntuMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UbuntuMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UbuntuMachine{}, &UbuntuMachineList{})
}
