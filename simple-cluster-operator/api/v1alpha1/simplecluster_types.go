/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SimpleClusterSpec defines the desired state of SimpleCluster.
type SimpleClusterSpec struct {
	// Workers is the number of worker nodes; surfaced as .SC.workers in the template.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Workers int32 `json:"workers"`

	// ProviderConfigRef pins a specific SimpleClusterProviderConfig.
	// When omitted, the highest-priority SCPC is selected automatically and
	// then locked into status.providerConfigRef so churn does not migrate Rs.
	// +optional
	ProviderConfigRef *corev1.LocalObjectReference `json:"providerConfigRef,omitempty"`

	// Inputs are per-cluster overrides. Every key MUST be declared in
	// SCPC.spec.inputs; unknown keys are rejected. Keys left unset fall back
	// to the SCPC declaration's default.
	// +optional
	Inputs map[string]string `json:"inputs,omitempty"`

	// KubeconfigValidity overrides SCPC.kubeconfigSource.validity for this SC.
	// +optional
	KubeconfigValidity *metav1.Duration `json:"kubeconfigValidity,omitempty"`

	// KubeconfigRenewalBefore overrides SCPC.kubeconfigSource.renewalBefore for this SC.
	// +optional
	KubeconfigRenewalBefore *metav1.Duration `json:"kubeconfigRenewalBefore,omitempty"`
}

// SimpleClusterStatus defines the observed state of SimpleCluster.
type SimpleClusterStatus struct {
	// ProviderConfigRef is locked at first reconcile so priority churn does
	// not migrate Rs across SCPCs.
	// +optional
	ProviderConfigRef *corev1.LocalObjectReference `json:"providerConfigRef,omitempty"`

	// TargetGVK is the resolved GroupVersionKind of R.
	// +optional
	TargetGVK string `json:"targetGVK,omitempty"`
	// TargetName is the name of R on the target cluster.
	// +optional
	TargetName string `json:"targetName,omitempty"`
	// TargetNamespace is the namespace of R on the target cluster.
	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`
	// TargetCluster identifies where R lives: SCPC name (for remote) or "local".
	// +optional
	TargetCluster string `json:"targetCluster,omitempty"`

	// Phase is the coarse state machine.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// LastOperation mirrors a human-readable progress string from R.
	// +optional
	LastOperation string `json:"lastOperation,omitempty"`
	// LastError mirrors a human-readable error string from R.
	// +optional
	LastError string `json:"lastError,omitempty"`

	// KubeconfigSecretRef points at the Secret in SC.namespace holding NC's kubeconfig.
	// +optional
	KubeconfigSecretRef *corev1.LocalObjectReference `json:"kubeconfigSecretRef,omitempty"`
	// KubeconfigIssuedAt is the issuance time of the embedded credential.
	// +optional
	KubeconfigIssuedAt *metav1.Time `json:"kubeconfigIssuedAt,omitempty"`
	// KubeconfigExpiresAt is the expiry time of the embedded credential.
	// +optional
	KubeconfigExpiresAt *metav1.Time `json:"kubeconfigExpiresAt,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sc
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.status.providerConfigRef.name`
// +kubebuilder:printcolumn:name="Workers",type=integer,JSONPath=`.spec.workers`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.status.targetName`
// +kubebuilder:printcolumn:name="Last Operation",type=string,JSONPath=`.status.lastOperation`
// +kubebuilder:printcolumn:name="Kubeconfig Expires",type=string,JSONPath=`.status.kubeconfigExpiresAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SimpleCluster is the user-facing namespaced cluster request.
type SimpleCluster struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec SimpleClusterSpec `json:"spec"`
	// +optional
	Status SimpleClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// SimpleClusterList contains a list of SimpleCluster.
type SimpleClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SimpleCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SimpleCluster{}, &SimpleClusterList{})
}
