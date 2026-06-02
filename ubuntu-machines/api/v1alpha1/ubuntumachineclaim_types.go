/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=Pending;Ready
type ClaimPhase string

const (
	// ClaimPending means the operator has not yet found an eligible
	// UbuntuMachine to satisfy the claim.
	ClaimPending ClaimPhase = "Pending"
	// ClaimReady means status.machine is populated and the bound
	// UbuntuMachine carries `status.boundClaimRef -> this claim`.
	// Matches the value SCPC consumers expect when checking phase.
	ClaimReady ClaimPhase = "Ready"
)

// UbuntuMachineClaimSpec is the consumer's ask: "give me one machine
// that fits these resource requirements". Mirrors PVC: one claim binds
// exactly one UbuntuMachine. The claim only ever resolves within its
// own namespace.
type UbuntuMachineClaimSpec struct {
	// ExternalAddressRequired filters the candidate set to machines
	// whose spec.externalAddress is non-empty. Use this for control-plane
	// claims where the consumer needs to reach the host directly.
	// +optional
	ExternalAddressRequired bool `json:"externalAddressRequired,omitempty"`

	// Required is the per-machine resource floor. A machine is eligible
	// iff `capacity.cpu >= required.cpu && capacity.memory >= required.memory`.
	// Zero / omitted means no constraint on that dimension.
	// +optional
	Required NodeResources `json:"required,omitempty"`

	// Selector restricts the candidate UbuntuMachines to those whose labels
	// match all of these key/value pairs (equality-based matchLabels).
	// Empty selects from the whole pool. Use this to pin a cluster to a
	// subset of the machine pool (e.g. a rack, zone, or tenant).
	// +optional
	Selector map[string]string `json:"selector,omitempty"`
}

// BoundMachine is the embedded copy of a bound UbuntuMachine's spec
// (just what consumers need to template against). Avoids forcing
// downstream operators to do a second .Resources lookup.
type BoundMachine struct {
	// Name is the bound UbuntuMachine's metadata.name.
	Name string `json:"name"`
	// Address echoes UbuntuMachine.spec.address.
	Address string `json:"address"`
	// ExternalAddress echoes UbuntuMachine.spec.externalAddress.
	// +optional
	ExternalAddress string `json:"externalAddress,omitempty"`
	// SSH echoes UbuntuMachine.spec.ssh.
	SSH SSHAuth `json:"ssh"`
}

// UbuntuMachineClaimStatus is the observed state.
type UbuntuMachineClaimStatus struct {
	// Phase is Pending until a machine is bound, then Ready.
	// +optional
	Phase ClaimPhase `json:"phase,omitempty"`

	// Machine is the embedded snapshot of the bound UbuntuMachine.
	// Nil when Pending.
	// +optional
	Machine *BoundMachine `json:"machine,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=umc
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Machine",type=string,JSONPath=`.status.machine.name`
// +kubebuilder:printcolumn:name="External",type=boolean,JSONPath=`.spec.externalAddressRequired`
// +kubebuilder:printcolumn:name="CPU",type=string,JSONPath=`.spec.required.cpu`
// +kubebuilder:printcolumn:name="Memory",type=string,JSONPath=`.spec.required.memory`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UbuntuMachineClaim is the consumer-facing CR: "give me one machine".
// Modeled after PersistentVolumeClaim — one claim binds one
// UbuntuMachine. Independent of any cluster technology — the bound
// machine can be used for anything reachable over SSH.
type UbuntuMachineClaim struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec UbuntuMachineClaimSpec `json:"spec"`
	// +optional
	Status UbuntuMachineClaimStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type UbuntuMachineClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UbuntuMachineClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UbuntuMachineClaim{}, &UbuntuMachineClaimList{})
}
