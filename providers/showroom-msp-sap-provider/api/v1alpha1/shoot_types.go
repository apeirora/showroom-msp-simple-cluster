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

// ShootStatus reports the provider config materialised for the namespace.
type ShootStatus struct {
	// ProviderConfigs lists the SimpleClusterProviderConfig names created in
	// this namespace on behalf of this flavor. SimpleClusters reference these
	// names via providerConfigRefs.
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
// +kubebuilder:resource:shortName=osshoot
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OpenStackShoot enables the SAP Gardener OpenStack flavor as a SimpleCluster
// provider for one namespace (KCP account workspace). Its spec is the Gardener
// Shoot manifest, as free text, that overrides the resource template of the
// sap-gardener-openstack SimpleClusterProviderConfig — the MSP side builds the
// actual Gardener Shoot from it.
type OpenStackShoot struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the Gardener Shoot manifest text used verbatim as the SCPC
	// resource template. It may contain simple-cluster template expressions
	// (Go-template placeholders evaluated later against the SimpleCluster and
	// its resolved inputs). When empty the operator's built-in default
	// OpenStack template is used.
	// +optional
	Spec string `json:"spec,omitempty"`

	// +optional
	Status ShootStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OpenStackShootList contains a list of OpenStackShoot.
type OpenStackShootList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenStackShoot `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=icshoot
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// IronCoreShoot enables the SAP Gardener IronCore flavor as a SimpleCluster
// provider for one namespace (KCP account workspace). Its spec is the Gardener
// Shoot manifest, as free text, that overrides the resource template of the
// sap-gardener-ironcore SimpleClusterProviderConfig — the MSP side builds the
// actual Gardener Shoot from it.
type IronCoreShoot struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the Gardener Shoot manifest text used verbatim as the SCPC
	// resource template. It may contain simple-cluster template expressions
	// (Go-template placeholders evaluated later against the SimpleCluster and
	// its resolved inputs). When empty the operator's built-in default
	// IronCore template is used.
	// +optional
	Spec string `json:"spec,omitempty"`

	// +optional
	Status ShootStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IronCoreShootList contains a list of IronCoreShoot.
type IronCoreShootList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IronCoreShoot `json:"items"`
}

// GetSpecText / GetShootStatus expose the shared surface both flavor CRs
// present to the generic per-flavor reconciler.
func (s *OpenStackShoot) GetSpecText() string          { return s.Spec }
func (s *OpenStackShoot) GetShootStatus() *ShootStatus { return &s.Status }
func (s *IronCoreShoot) GetSpecText() string           { return s.Spec }
func (s *IronCoreShoot) GetShootStatus() *ShootStatus  { return &s.Status }

func init() {
	SchemeBuilder.Register(
		&OpenStackShoot{}, &OpenStackShootList{},
		&IronCoreShoot{}, &IronCoreShootList{},
	)
}
