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

// ShootStatus reports the Gardener cluster provisioned for this CR and the
// provider config materialised for the namespace.
type ShootStatus struct {
	// ProviderConfigs lists the SimpleClusterProviderConfig names created in
	// this namespace on behalf of this flavor. SimpleClusters reference these
	// names via providerConfigRefs.
	// +optional
	ProviderConfigs []string `json:"providerConfigs,omitempty"`

	// ShootName is the name of the Gardener Shoot provisioned for this CR.
	// +optional
	ShootName string `json:"shootName,omitempty"`

	// Phase mirrors the Gardener Shoot lifecycle: Provisioning, Ready, Failed
	// or Deleting.
	// +optional
	Phase string `json:"phase,omitempty"`

	// KubeconfigSecretRef names the Secret (next to this CR) holding the admin
	// kubeconfig for the provisioned cluster.
	// +optional
	KubeconfigSecretRef *KubeconfigSecretRef `json:"kubeconfigSecretRef,omitempty"`

	// KubeconfigExpires is when the minted admin kubeconfig expires; the
	// operator renews it ahead of this time.
	// +optional
	KubeconfigExpires *metav1.Time `json:"kubeconfigExpires,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KubeconfigSecretRef points at the kubeconfig Secret in the CR's namespace.
type KubeconfigSecretRef struct {
	Name string `json:"name"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=osshoot
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OpenStackShoot provisions a Gardener cluster of the SAP Integration
// OpenStack flavor for one namespace (KCP account workspace): the operator
// creates a real Gardener Shoot from it and maintains an admin-kubeconfig
// Secret next to the CR.
type OpenStackShoot struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is a plain Gardener Shoot manifest, as free text, used verbatim for
	// the provisioned cluster. When empty the operator's built-in default
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
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// IronCoreShoot provisions a Gardener cluster of the SAP Integration IronCore
// flavor for one namespace (KCP account workspace): the operator creates a
// real Gardener Shoot from it and maintains an admin-kubeconfig Secret next
// to the CR.
type IronCoreShoot struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is a plain Gardener Shoot manifest, as free text, used verbatim for
	// the provisioned cluster. When empty the operator's built-in default
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
