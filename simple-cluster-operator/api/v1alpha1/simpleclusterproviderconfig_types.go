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

// +kubebuilder:validation:Enum=string;int;bool;duration
type InputType string

const (
	InputTypeString   InputType = "string"
	InputTypeInt      InputType = "int"
	InputTypeBool     InputType = "bool"
	InputTypeDuration InputType = "duration"
)

// InputDeclaration is a single SC input slot: name, type, default, and metadata
// to make the SCPC self-documenting (kubectl explain / UI rendering).
type InputDeclaration struct {
	// Name as referenced from the template (e.g. region -> {{ .Inputs.region }}).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_]*$`
	Name string `json:"name"`

	// Type of the input. Both the Default and any SC override are parsed
	// against this type; mismatches are rejected at reconcile.
	// +kubebuilder:default=string
	// +optional
	Type InputType `json:"type,omitempty"`

	// Default value substituted when SC.spec.inputs[Name] is unset.
	// Stored as string for kubectl ergonomics regardless of Type;
	// parsing happens at reconcile.
	Default string `json:"default"`

	// Description shown in CRD docs / UI tooling.
	// +optional
	Description string `json:"description,omitempty"`

	// AllowEmpty permits "" as a meaningful value for Type=string.
	// Ignored for other types (empty never parses as int/bool/duration).
	// +optional
	AllowEmpty bool `json:"allowEmpty,omitempty"`
}

// TargetSpec describes WHERE and WHAT to create.
type TargetSpec struct {
	// APIVersion of the target resource R (e.g. core.gardener.cloud/v1beta1).
	APIVersion string `json:"apiVersion"`

	// Kind of the target resource R (e.g. Shoot).
	Kind string `json:"kind"`

	// Namespace on the target cluster where R is created.
	// Required for namespaced kinds, ignored for cluster-scoped.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// NamePrefix prepended to the generated R name (default: SC.name).
	// +optional
	NamePrefix string `json:"namePrefix,omitempty"`

	// KubeconfigSecretRef points at a Secret in the management cluster
	// whose "kubeconfig" key holds credentials for the target cluster.
	// When nil, R is created on the management cluster.
	// +optional
	KubeconfigSecretRef *corev1.SecretReference `json:"kubeconfigSecretRef,omitempty"`

	// DeletionAnnotations are applied to R before the operator attempts to
	// delete it. Required by some controllers as a "double opt-in"
	// (e.g. Gardener Shoot needs `confirmation.gardener.cloud/deletion: "true"`).
	// +optional
	DeletionAnnotations map[string]string `json:"deletionAnnotations,omitempty"`
}

// KubeconfigSource describes how to obtain the kubeconfig of NC once R is Ready.
// Exactly one of SecretRef / SubResource / Inline must be set.
type KubeconfigSource struct {
	// SecretRef - kubeconfig lives in a Secret in the target cluster's namespace
	// (typical for Cluster API / Gardener seed Secrets). Name/Namespace are Go
	// templates evaluated against the same context as Template so they can refer to R.
	// +optional
	SecretRef *KubeconfigSecretSource `json:"secretRef,omitempty"`

	// SubResource - call a SubResource on R (e.g. Gardener's /adminkubeconfig).
	// +optional
	SubResource *KubeconfigSubResourceSource `json:"subResource,omitempty"`

	// Inline - JSONPath expression that locates the kubeconfig bytes inside R itself.
	// +optional
	Inline *KubeconfigInlineSource `json:"inline,omitempty"`

	// Validity is the desired lifetime of issued kubeconfigs (SubResource only).
	// +optional
	Validity *metav1.Duration `json:"validity,omitempty"`

	// RenewalBefore is how long before expiry the kubeconfig is renewed (SubResource only).
	// +optional
	RenewalBefore *metav1.Duration `json:"renewalBefore,omitempty"`
}

type KubeconfigSecretSource struct {
	// Name of the Secret; Go template.
	Name string `json:"name"`
	// Namespace of the Secret; Go template; defaults to R.namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// DataKey is the key in the Secret holding the kubeconfig; defaults to "kubeconfig".
	// +optional
	DataKey string `json:"dataKey,omitempty"`
}

type KubeconfigSubResourceSource struct {
	// APIVersion of the request object (e.g. authentication.gardener.cloud/v1alpha1).
	APIVersion string `json:"apiVersion"`
	// Kind of the request object (e.g. AdminKubeconfigRequest).
	Kind string `json:"kind"`
	// SubResource name (e.g. adminkubeconfig).
	SubResource string `json:"subResource"`
	// KubeconfigPath inside the response that holds the kubeconfig bytes (e.g. status.kubeconfig).
	KubeconfigPath string `json:"kubeconfigPath"`
	// ExpirationPath inside the response that holds the expiry timestamp.
	// +optional
	ExpirationPath string `json:"expirationPath,omitempty"`
}

type KubeconfigInlineSource struct {
	// KubeconfigPath is the JSONPath into R that resolves to kubeconfig bytes.
	KubeconfigPath string `json:"kubeconfigPath"`
}

// StatusMap projects the target resource's status into the universal Phase enum.
// Optional - when omitted, the operator walks R.status.conditions[type=Ready].
type StatusMap struct {
	// PhasePath is a JSONPath into R that resolves to a state value (e.g. status.lastOperation.state).
	PhasePath string `json:"phasePath"`
	// PhaseReadyValue is the value at PhasePath that indicates Ready (e.g. Succeeded).
	PhaseReadyValue string `json:"phaseReadyValue"`
	// PhaseFailedValues are values at PhasePath that indicate Failed.
	// +optional
	PhaseFailedValues []string `json:"phaseFailedValues,omitempty"`
	// LastOperationPath optionally provides a human-readable progress string.
	// +optional
	LastOperationPath string `json:"lastOperationPath,omitempty"`
	// LastErrorPath optionally provides a human-readable error string.
	// +optional
	LastErrorPath string `json:"lastErrorPath,omitempty"`
}

// SimpleClusterProviderConfigSpec defines the desired state of SimpleClusterProviderConfig.
type SimpleClusterProviderConfigSpec struct {
	// Target describes WHERE and WHAT to create.
	Target TargetSpec `json:"target"`

	// Template is the YAML body of resource R rendered through the Helm-style
	// text/template + sprig/v3 engine. The post-render result MUST embed
	// apiVersion/kind consistent with Target.
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template"`

	// KubeconfigSource describes HOW to obtain the kubeconfig of NC once R is Ready.
	KubeconfigSource KubeconfigSource `json:"kubeconfigSource"`

	// Inputs is the closed set of keys a SimpleCluster may override via spec.inputs.
	// Every entry MUST carry a default - the template always sees a fully-populated value.
	// +optional
	// +listType=map
	// +listMapKey=name
	Inputs []InputDeclaration `json:"inputs,omitempty"`

	// StatusMap projects R's status into Phase. Optional; defaults to the
	// Ready-condition walker when omitted.
	// +optional
	StatusMap *StatusMap `json:"statusMap,omitempty"`
}

// SimpleClusterProviderConfigStatus defines the observed state of SimpleClusterProviderConfig.
type SimpleClusterProviderConfigStatus struct {
	// Reachable mirrors a smoke test against Target.KubeconfigSecretRef.
	// True when omitted (local cluster).
	// +optional
	Reachable *bool `json:"reachable,omitempty"`

	// Target is a friendly summary of where R will be created: "<namespace>/<kind>"
	// for namespaced targets, just "<kind>" for cluster-scoped ones. Populated
	// after the GVK is successfully resolved on the target cluster.
	// +optional
	Target string `json:"target,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=scpc
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.status.target`
// +kubebuilder:printcolumn:name="Reachable",type=boolean,JSONPath=`.status.reachable`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SimpleClusterProviderConfig is the cluster-scoped admin-facing template/profile.
type SimpleClusterProviderConfig struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec SimpleClusterProviderConfigSpec `json:"spec"`
	// +optional
	Status SimpleClusterProviderConfigStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// SimpleClusterProviderConfigList contains a list of SimpleClusterProviderConfig.
type SimpleClusterProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SimpleClusterProviderConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SimpleClusterProviderConfig{}, &SimpleClusterProviderConfigList{})
}
