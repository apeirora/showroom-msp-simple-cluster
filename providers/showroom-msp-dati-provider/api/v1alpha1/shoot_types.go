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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ShootStatus reports the provider configs materialised for the namespace.
type ShootStatus struct {
	// ProviderConfigs lists the SimpleClusterProviderConfig names created in
	// this namespace. SimpleClusters reference these names via providerConfigRefs.
	// +optional
	ProviderConfigs []string `json:"providerConfigs,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Shoot enables dati as a SimpleCluster provider for one namespace (KCP
// account workspace). Its spec is the Gardener Shoot manifest, as free text,
// that the resulting SimpleClusterProviderConfig uses as its resource
// template — the MSP side builds the actual Gardener Shoot from it.
type Shoot struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the Gardener Shoot manifest text used verbatim as the SCPC
	// resource template. It may contain simple-cluster template expressions
	// (Go-template placeholders evaluated later against the SimpleCluster and
	// its resolved inputs). When empty the operator's built-in default
	// template is used.
	// +optional
	Spec string `json:"spec,omitempty"`

	// +optional
	Status ShootStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ShootList contains a list of Shoot.
type ShootList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Shoot `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Shoot{}, &ShootList{})
}
