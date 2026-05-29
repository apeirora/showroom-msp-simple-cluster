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
	FinalizerName = "kindcluster.apeirora.eu/finalizer"

	ConditionReady = "Ready"
	ConditionError = "Error"
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

// KindClusterSpec is what the user (or upstream operator) asks for.
type KindClusterSpec struct {
	// ClusterName is the name passed to `kind create cluster --name`.
	// Defaults to metadata.name when empty.
	// +optional
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	// +kubebuilder:validation:MaxLength=50
	ClusterName string `json:"clusterName,omitempty"`

	// Workers is the number of worker nodes in addition to the single
	// control-plane node. 0 = single-node cluster.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=20
	// +kubebuilder:default=0
	Workers int32 `json:"workers,omitempty"`

	// NodeImage pins the Kubernetes version (a kindest/node tag).
	// Empty = kind's compiled-in default for the running kind version.
	// +optional
	NodeImage string `json:"nodeImage,omitempty"`
}

// KindClusterStatus is the operator's view of the cluster.
type KindClusterStatus struct {
	// Phase is the coarse state.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ClusterName mirrors the effective name once locked in.
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// KubeconfigSecretRef references the Secret in this namespace
	// holding the cluster's kubeconfig under key "kubeconfig".
	// +optional
	KubeconfigSecretRef *corev1.LocalObjectReference `json:"kubeconfigSecretRef,omitempty"`

	// CP is "<readyControlPlanes>/<declaredControlPlanes>" — surfaced
	// as the CP printer column (e.g. "1/1"). kind is hardcoded to a
	// single control-plane node.
	// +optional
	CP string `json:"cp,omitempty"`

	// WKR is "<readyWorkers>/<declaredWorkers>" — surfaced as the
	// WKR printer column (e.g. "0/2").
	// +optional
	WKR string `json:"wkr,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kindc
// +kubebuilder:printcolumn:name="CP",type=string,JSONPath=`.status.cp`
// +kubebuilder:printcolumn:name="WKR",type=string,JSONPath=`.status.wkr`
// +kubebuilder:printcolumn:name="Kubeconfig",type=string,JSONPath=`.status.kubeconfigSecretRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KindCluster requests creation of a standalone KinD cluster on the
// host's docker daemon (DooD via the operator's mounted docker socket).
type KindCluster struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec KindClusterSpec `json:"spec"`
	// +optional
	Status KindClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type KindClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KindCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KindCluster{}, &KindClusterList{})
}
