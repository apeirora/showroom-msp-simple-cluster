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
	FinalizerName = "arsysubuntumachine.apeirora.eu/finalizer"

	ConditionReady = "Ready"
	ConditionError = "Error"

	// TokenSecretKey is the data key in spec.tokenSecretRef holding the
	// Arsys CloudBuilder API token (sent as the X-TOKEN header).
	TokenSecretKey = "token"
)

// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Failed;Deleting
type Phase string

const (
	// PhasePending: picked up, finalizer set, server not yet ordered.
	PhasePending Phase = "Pending"
	// PhaseProvisioning: server ordered at Arsys, waiting for it to come up.
	PhaseProvisioning Phase = "Provisioning"
	// PhaseReady: server is up and the owned UbuntuMachine exists.
	PhaseReady Phase = "Ready"
	// PhaseFailed: a terminal/transient error — see the Error condition.
	PhaseFailed Phase = "Failed"
	// PhaseDeleting: deletion in flight (machine + server teardown).
	PhaseDeleting Phase = "Deleting"
)

// ArsysUbuntuMachineSpec describes a dedicated server to order from Arsys
// CloudBuilder and the UbuntuMachine to register once it is up.
type ArsysUbuntuMachineSpec struct {
	// TokenSecretRef points at the Secret holding the Arsys API token under
	// the `token` key (sent as X-TOKEN). You provide this Secret; the
	// operator only reads it. Namespaced because this CR is cluster-scoped.
	TokenSecretRef corev1.SecretReference `json:"tokenSecretRef"`

	// PasswordSecretRef names the Secret the operator WRITES the server's
	// generated root password into after provisioning (kubernetes.io/basic-auth,
	// keys `username`/`password`). The operator creates it if absent; the
	// UbuntuMachine references it for SSH. Namespaced.
	PasswordSecretRef corev1.SecretReference `json:"passwordSecretRef"`

	// Server is the dedicated-server order, mapped 1:1 onto the Arsys
	// `POST /servers` request.
	Server ServerSpec `json:"server"`

	// SSH is the login the resulting UbuntuMachine advertises.
	SSH SSHSpec `json:"ssh"`
}

// +kubebuilder:validation:Enum=cloud;dedicated
type ServerType string

const (
	// ServerTypeCloud is a virtual server sized by vcore/ram/disk.
	ServerTypeCloud ServerType = "cloud"
	// ServerTypeDedicated is a bare-metal server sized by a baremetal model.
	ServerTypeDedicated ServerType = "dedicated"
)

// ServerSpec mirrors the Arsys CloudBuilder server order body. A dedicated
// (bare-metal) order is sized by BaremetalModelID; a cloud (virtual) order by
// vcore/coresPerProcessor/ram/disk.
// +kubebuilder:validation:XValidation:rule="self.type != 'dedicated' || (has(self.baremetalModelID) && self.baremetalModelID != '') || (has(self.vcore) && has(self.ram))",message="dedicated requires baremetalModelID, or vcore and ram to auto-select the smallest model"
// +kubebuilder:validation:XValidation:rule="self.type != 'cloud' || (has(self.vcore) && has(self.ram) && has(self.disk))",message="vcore, ram and disk are required when type is cloud"
type ServerSpec struct {
	// Name is the server name at Arsys. Defaults to metadata.name.
	// +optional
	// +kubebuilder:validation:MaxLength=128
	Name string `json:"name,omitempty"`

	// Type selects a dedicated (bare-metal) or cloud (virtual) server.
	// +kubebuilder:default=dedicated
	Type ServerType `json:"type,omitempty"`

	// ApplianceID selects the OS image/appliance (an Ubuntu appliance).
	// +kubebuilder:validation:MinLength=1
	ApplianceID string `json:"applianceID"`

	// DatacenterID selects the datacenter the server is created in.
	// +kubebuilder:validation:MinLength=1
	DatacenterID string `json:"datacenterID"`

	// BaremetalModelID picks the bare-metal model (e.g. "BMC_S") for a
	// dedicated order. When omitted the operator auto-selects the smallest
	// ENABLED model meeting vcore/ram that is available in datacenterID.
	// List with GET /servers/baremetal_models.
	// +optional
	BaremetalModelID string `json:"baremetalModelID,omitempty"`

	// VCore is the number of cores: the exact size for a cloud order, or the
	// minimum cores to match when auto-selecting a dedicated model.
	// +optional
	VCore int32 `json:"vcore,omitempty"`

	// CoresPerProcessor is the core/processor layout (cloud only).
	// +optional
	CoresPerProcessor int32 `json:"coresPerProcessor,omitempty"`

	// RAM is memory in GiB: the exact size for a cloud order, or the minimum
	// RAM to match when auto-selecting a dedicated model.
	// +optional
	RAM int32 `json:"ram,omitempty"`

	// Disk is the main disk size in GiB (cloud only).
	// +optional
	Disk int32 `json:"disk,omitempty"`

	// PublicKeys are SSH public keys pushed to the server at order time.
	// Usually empty — keys are managed in the Arsys CloudPanel.
	// +optional
	PublicKeys []string `json:"publicKeys,omitempty"`
}

// SSHSpec is the login the UbuntuMachine advertises to consumers.
type SSHSpec struct {
	// User is the sudo-capable login (e.g. "root", "ubuntu").
	// +kubebuilder:validation:MinLength=1
	User string `json:"user"`
}

// ArsysUbuntuMachineStatus is the observed state.
type ArsysUbuntuMachineStatus struct {
	// Phase is the coarse state.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ServerID is the Arsys server id, set once the order is accepted.
	// +optional
	ServerID string `json:"serverID,omitempty"`

	// ServerName is the Arsys server name used for this CR. Written once before
	// the first create attempt so it remains stable across retries.
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// BaremetalModelID is the dedicated model the server was ordered with.
	// When spec.server.baremetalModelID was empty this is the auto-selected
	// smallest model that met vcore/ram in the datacenter.
	// +optional
	BaremetalModelID string `json:"baremetalModelID,omitempty"`

	// SelectedCores and SelectedRAMGB are the auto-selected model's actual
	// size, mirrored onto the UbuntuMachine capacity. Unset for explicit-model
	// or cloud orders.
	// +optional
	SelectedCores int32 `json:"selectedCores,omitempty"`
	// +optional
	SelectedRAMGB int32 `json:"selectedRAMGB,omitempty"`

	// ProvisioningPercent is the server build progress reported by the Arsys API (0-100).
	// Cleared once the server reaches Ready.
	// +optional
	ProvisioningPercent int `json:"provisioningPercent,omitempty"`

	// Address is the server's public IP once assigned.
	// +optional
	Address string `json:"address,omitempty"`

	// MachineRef is the name of the UbuntuMachine the operator created.
	// +optional
	MachineRef string `json:"machineRef,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=aum
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.server.type`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Progress",type=integer,JSONPath=`.status.provisioningPercent`
// +kubebuilder:printcolumn:name="Server",type=string,JSONPath=`.status.serverID`
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.status.address`
// +kubebuilder:printcolumn:name="Machine",type=string,JSONPath=`.status.machineRef`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ArsysUbuntuMachine orders a dedicated server from Arsys CloudBuilder and
// registers it as a cluster-scoped UbuntuMachine (owned by this CR). The
// server is deleted when the CR is deleted.
type ArsysUbuntuMachine struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec ArsysUbuntuMachineSpec `json:"spec"`
	// +optional
	Status ArsysUbuntuMachineStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type ArsysUbuntuMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ArsysUbuntuMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ArsysUbuntuMachine{}, &ArsysUbuntuMachineList{})
}
