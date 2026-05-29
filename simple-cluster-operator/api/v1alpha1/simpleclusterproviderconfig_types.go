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

// +kubebuilder:validation:Enum=string;int;bool;duration;enum
type InputType string

const (
	InputTypeString   InputType = "string"
	InputTypeInt      InputType = "int"
	InputTypeBool     InputType = "bool"
	InputTypeDuration InputType = "duration"
	InputTypeEnum     InputType = "enum"
)

// InputEnumValue is one allowed choice for an enum-typed input.
type InputEnumValue struct {
	// Key is the value substituted into the template (e.g. "eu-de-1").
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
	// Label is the human-readable description shown in UIs / kubectl explain.
	// +optional
	Label string `json:"label,omitempty"`
}

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
	Default string `json:"default"`

	// Description shown in CRD docs / UI tooling.
	// +optional
	Description string `json:"description,omitempty"`

	// AllowEmpty permits "" as a meaningful value for Type=string.
	// +optional
	AllowEmpty bool `json:"allowEmpty,omitempty"`

	// Values enumerates the allowed choices when Type=enum. Required and
	// non-empty for enum inputs; ignored otherwise. Default (if set) and any
	// SC override must match one of Values[].Key.
	// +optional
	// +listType=map
	// +listMapKey=key
	Values []InputEnumValue `json:"values,omitempty"`
}

// TargetSpec describes which cluster the rendered resources are created on
// and the default namespace for namespaced resources. Per-resource overrides
// (apiVersion/kind, namespace, namePrefix, deletionAnnotations) live on
// each ResourceSpec.
type TargetSpec struct {
	// Namespace used by default for namespaced resources. Each ResourceSpec
	// may override this via its own Namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// KubeconfigSecretRef points at a Secret in the management cluster
	// whose "kubeconfig" key holds credentials for the target cluster.
	// When nil, resources are created on the management cluster.
	// +optional
	KubeconfigSecretRef *corev1.SecretReference `json:"kubeconfigSecretRef,omitempty"`
}

// ResourceSpec describes one node in the SCPC's DAG: a YAML template plus
// dependency and readiness metadata.
type ResourceSpec struct {
	// Name is the resource's identity within this SCPC. Referenced from
	// other resources via .Resources.<name> in their templates, and from
	// KubeconfigSource.ResourceName.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	Name string `json:"name"`

	// APIVersion of the target resource R (e.g. core.gardener.cloud/v1beta1).
	APIVersion string `json:"apiVersion"`

	// Kind of the target resource R (e.g. Shoot).
	Kind string `json:"kind"`

	// Namespace overrides Target.Namespace for this resource.
	// Required for namespaced kinds when Target.Namespace is also empty.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// NamePrefix prepended to the generated R name. The actual name is
	// sanitise(<NamePrefix><SC.name>-<Name>).
	// +optional
	NamePrefix string `json:"namePrefix,omitempty"`

	// Template is the YAML body of R rendered through the Helm-style
	// text/template + sprig/v3 engine. The post-render result MUST embed
	// apiVersion/kind consistent with this ResourceSpec.
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template"`

	// Repeat, when set, fans this ResourceSpec out to N target resources.
	// The value is a Go template (same engine as Template, with .SC /
	// .Inputs / .Resources in scope but NOT .Index / .Prev) that MUST
	// render to a non-negative integer. Target names get suffixed with
	// "-<index>" (0-based). Inside Template, .Index is the current
	// iteration and .Prev is the list of objects rendered so far for this
	// ResourceSpec. From sibling ResourceSpecs' Templates, .Resources.<name>
	// is exposed as a list (rather than a single object) when <name> had
	// Repeat set.
	// +optional
	Repeat string `json:"repeat,omitempty"`

	// DependsOn is the list of sibling ResourceSpec names that must reach
	// Ready before this resource is rendered. Cycles, unknown names, and
	// self-references are rejected at reconcile-time.
	// +optional
	DependsOn []string `json:"dependsOn,omitempty"`

	// Readiness projects R's status into the universal Phase enum.
	// Optional - when omitted, the operator reads R.status.phase and treats
	// "Ready" as Ready, "Failed"/"Error" as Failed, empty/missing as
	// Pending, and any other value as Provisioning.
	// +optional
	Readiness *ReadinessCheck `json:"readiness,omitempty"`

	// DeletionAnnotations are applied to R before the operator deletes it.
	// Required by some controllers as a "double opt-in"
	// (e.g. Gardener Shoot needs `confirmation.gardener.cloud/deletion: "true"`).
	// +optional
	DeletionAnnotations map[string]string `json:"deletionAnnotations,omitempty"`
}

// ReadinessCheck (formerly StatusMap) projects a target resource's status
// onto the universal Phase enum.
type ReadinessCheck struct {
	// PhasePath is a JSONPath into R that resolves to a state value
	// (e.g. status.lastOperation.state).
	PhasePath string `json:"phasePath"`

	// PhaseReadyValue is the value at PhasePath that indicates Ready
	// (e.g. Succeeded).
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

// KubeconfigSource describes how to obtain the kubeconfig of NC once the
// resource named by ResourceName is Ready.
// Exactly one of SecretRef / SubResource / Inline must be set.
type KubeconfigSource struct {
	// ResourceName names the ResourceSpec whose kubeconfig is synced to SC.
	// Must match one of SCPC.spec.resources[].name.
	// +kubebuilder:validation:MinLength=1
	ResourceName string `json:"resourceName"`

	// SecretRef - kubeconfig lives in a Secret in the target cluster's
	// namespace (typical for Cluster API / Gardener seed Secrets). Name/
	// Namespace are Go templates evaluated against the same context as the
	// named resource's Template, with the live R bound at
	// .Resources.<resourceName>.
	// +optional
	SecretRef *KubeconfigSecretSource `json:"secretRef,omitempty"`

	// SubResource - call a SubResource on R (e.g. Gardener's /adminkubeconfig).
	// +optional
	SubResource *KubeconfigSubResourceSource `json:"subResource,omitempty"`

	// Inline - JSONPath expression that locates the kubeconfig bytes inside
	// R itself.
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
	APIVersion     string `json:"apiVersion"`
	Kind           string `json:"kind"`
	SubResource    string `json:"subResource"`
	KubeconfigPath string `json:"kubeconfigPath"`
	// +optional
	ExpirationPath string `json:"expirationPath,omitempty"`
}

type KubeconfigInlineSource struct {
	// KubeconfigPath is the JSONPath into R that resolves to kubeconfig bytes.
	KubeconfigPath string `json:"kubeconfigPath"`
}

// SimpleClusterProviderConfigSpec defines the desired state of SimpleClusterProviderConfig.
type SimpleClusterProviderConfigSpec struct {
	// Target describes which cluster the resources are created on plus the
	// default Namespace.
	Target TargetSpec `json:"target"`

	// Resources is the ordered DAG of resources to render and create.
	// Edges are declared via ResourceSpec.DependsOn.
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	Resources []ResourceSpec `json:"resources"`

	// KubeconfigSource describes how to obtain the kubeconfig of NC once
	// the resource named by KubeconfigSource.ResourceName reports Ready.
	KubeconfigSource KubeconfigSource `json:"kubeconfigSource"`

	// Inputs is the closed set of keys a SimpleCluster may override via
	// spec.inputs. Every entry MUST carry a default.
	// +optional
	// +listType=map
	// +listMapKey=name
	Inputs []InputDeclaration `json:"inputs,omitempty"`
}

// ResolvedResource records a successfully resolved ResourceSpec on the target cluster.
type ResolvedResource struct {
	// Name matches ResourceSpec.Name.
	Name string `json:"name"`
	// GVK is the resolved GroupVersionKind ("<group>/<version>, Kind=<Kind>").
	GVK string `json:"gvk"`
}

// SimpleClusterProviderConfigStatus defines the observed state of SimpleClusterProviderConfig.
type SimpleClusterProviderConfigStatus struct {
	// Reachable mirrors a smoke test against Target.KubeconfigSecretRef.
	// True when omitted (local cluster).
	// +optional
	Reachable *bool `json:"reachable,omitempty"`

	// Resources lists each ResourceSpec's resolution outcome against the
	// target cluster. Populated when all GVKs resolve successfully.
	// +listType=map
	// +listMapKey=name
	// +optional
	Resources []ResolvedResource `json:"resources,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=scpc
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
