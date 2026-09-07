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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Profile is a named machine type with a fixed per-node CPU/memory capacity.
// Profiles are the closed set of machine types an SCPC offers; an input
// declared with From=profile resolves to the smallest profile that fits the
// SC's requested per-node resources.
type Profile struct {
	// Name is the machine type identifier substituted into templates
	// (e.g. "m5.large").
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// CPU is the profile's CPU capacity per node (e.g. "4", "8000m").
	CPU resource.Quantity `json:"cpu"`

	// Memory is the profile's memory capacity per node (e.g. "16Gi").
	Memory resource.Quantity `json:"memory"`
}

// +kubebuilder:validation:Enum=string;int;bool;duration;enum
type InputType string

const (
	InputTypeString   InputType = "string"
	InputTypeInt      InputType = "int"
	InputTypeBool     InputType = "bool"
	InputTypeDuration InputType = "duration"
	InputTypeEnum     InputType = "enum"
)

// +kubebuilder:validation:Enum=profile
type InputSource string

const (
	// InputSourceProfile resolves the input to the name of the smallest
	// SCPC profile (spec.profiles) that fits the SC's requested per-node
	// resources. The SimpleCluster lands in Phase=NoHardware when none fits.
	InputSourceProfile InputSource = "profile"
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
	// Name as referenced from the template (e.g. region -> .Inputs.region).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_]*$`
	Name string `json:"name"`

	// Type of the input. Both the Default and any SC override are parsed
	// against this type; mismatches are rejected at reconcile.
	// +kubebuilder:default=string
	// +optional
	Type InputType `json:"type,omitempty"`

	// Default value substituted when SC.spec.inputs[Name] is unset.
	// Required unless From is set.
	// +optional
	Default string `json:"default,omitempty"`

	// From derives the input's value from a source other than Default. With
	// From=profile the value is the name of the smallest SCPC profile that
	// fits the SC's requested per-node resources; such inputs need no Default
	// and no Type (always string). An explicit SC override is passed through
	// unchanged (free-form, not validated against the profile list).
	// +optional
	From InputSource `json:"from,omitempty"`

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
	// may override this via its own Namespace. May be a Go template evaluated
	// against .SC and .Inputs (e.g. `.SC.namespace` to co-locate
	// resources with the owning SimpleCluster).
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
	// May be a Go template evaluated against .SC and .Inputs.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// NamePrefix prepended to the generated R name. The actual name is
	// sanitise(<NamePrefix><SC.name>-<Name>).
	// +optional
	NamePrefix string `json:"namePrefix,omitempty"`

	// NameTemplate, when set, overrides NamePrefix and computes the R name
	// by rendering this Go template against .SC and .Inputs. The result is
	// used verbatim as the target resource name.
	// +optional
	NameTemplate string `json:"nameTemplate,omitempty"`

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
	// Repeat set. Crash cleanup can reconstruct repeat counts derived from
	// .SC and .Inputs. Deletion cannot rebuild predecessor context for a
	// count derived from .Resources; it deletes recorded owned instances and
	// retains the SimpleCluster finalizer for verified manual cleanup.
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
	// SimpleCluster ownership annotations and the core allow-delete marker are
	// reserved and rejected at reconcile time.
	// +optional
	DeletionAnnotations map[string]string `json:"deletionAnnotations,omitempty"`

	// ForceCleanupOnCascade, when true, tells the operator that R has no
	// external state worth tearing down gracefully in the context of a
	// SimpleCluster delete cascade: after issuing Delete the operator
	// will give R a short grace window and then strip all finalizers,
	// letting K8s GC it immediately. Use for layers whose graceful
	// teardown depends on infrastructure that is itself being deleted
	// in the same cascade (e.g. UbuntuCluster's SSH-based k3s uninstall
	// on VMs that are about to disappear). Do NOT set on resources that
	// own external state which must be reclaimed by their controller
	// (Gardener Shoot, OpenStackVM, …) — they will leak.
	// +optional
	ForceCleanupOnCascade bool `json:"forceCleanupOnCascade,omitempty"`
}

// ConditionMatch identifies one Kubernetes-style status condition by its
// exact type and status.
type ConditionMatch struct {
	// Type is the condition type (for example Ready or Synced).
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type"`

	// Status is the condition status to match.
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status"`
}

// ConditionReadinessCheck projects a Kubernetes-style status.conditions list
// onto the universal Phase enum without depending on list order.
type ConditionReadinessCheck struct {
	// Ready identifies the condition value that means the resource is Ready.
	Ready ConditionMatch `json:"ready"`

	// Failures identify condition values that mean the resource is Failed.
	// When several match simultaneously, all matching messages are surfaced.
	// +optional
	// +listType=map
	// +listMapKey=type
	Failures []ConditionMatch `json:"failures,omitempty"`

	// MatchObservedGeneration ignores a condition whose non-zero
	// observedGeneration differs from the target object's generation. Keep
	// this disabled for APIs synced across clusters whose source and target
	// generations belong to different domains.
	// +optional
	MatchObservedGeneration bool `json:"matchObservedGeneration,omitempty"`
}

// ReadinessCheck (formerly StatusMap) projects a target resource's status
// onto the universal Phase enum. Exactly one of the legacy PhasePath mapping
// and the typed Conditions mapping must be configured. The SCPC reconciler
// validates that mutual exclusion before resolving target resources.
type ReadinessCheck struct {
	// PhasePath is a JSONPath into R that resolves to a state value
	// (e.g. status.lastOperation.state).
	// +optional
	PhasePath string `json:"phasePath,omitempty"`

	// PhaseReadyValue is the value at PhasePath that indicates Ready
	// (e.g. Succeeded).
	// +optional
	PhaseReadyValue string `json:"phaseReadyValue,omitempty"`

	// PhaseFailedValues are values at PhasePath that indicate Failed.
	// +optional
	PhaseFailedValues []string `json:"phaseFailedValues,omitempty"`

	// LastOperationPath optionally provides a human-readable progress string.
	// +optional
	LastOperationPath string `json:"lastOperationPath,omitempty"`

	// LastErrorPath optionally provides a human-readable error string.
	// +optional
	LastErrorPath string `json:"lastErrorPath,omitempty"`

	// Conditions selects Kubernetes-style status conditions by type and
	// status. It is mutually exclusive with the legacy PhasePath mapping.
	// +optional
	Conditions *ConditionReadinessCheck `json:"conditions,omitempty"`
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
	// .Resources.<resourceName>. Metadata validation uses a strict
	// meta.k8s.io/v1 PartialObjectMetadata request with verified response
	// TypeMeta and without a full-object fallback. The target API server must
	// honor that negotiation and return canonical application/json carrying
	// meta.k8s.io/v1 PartialObjectMetadata.
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
	// RequiredLabels is an optional exact metadata requirement for the referenced Secret.
	// Values are Go templates evaluated with the same context as Name. When at least one
	// label is configured, the data request is a server-filtered list constrained by
	// namespace, metadata.name and every rendered label. This prevents a raced replacement
	// without those labels from returning its data. Metadata equality is not issuer
	// authentication: providers must protect these keys with admission/RBAC or a stronger
	// claim relation.
	// +optional
	RequiredLabels map[string]string `json:"requiredLabels,omitempty"`
	// RequiredAnnotations is the annotation counterpart of RequiredLabels. Values are
	// Go templates evaluated with the same context as Name and require the same external
	// writer-integrity guarantee. Kubernetes cannot server-filter a Secret list by
	// annotation; without at least one RequiredLabels entry, annotation checks and the
	// UID/resourceVersion guard prevent credential use but not a raced full-data transfer.
	// +optional
	RequiredAnnotations map[string]string `json:"requiredAnnotations,omitempty"`
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
	// ProvisioningEnabled controls whether this provider config may create new
	// target resources. It defaults to true so existing SCPCs keep their current
	// behaviour. Setting it to false retires the provider for new provisioning
	// while preserving the target and resource contract needed to observe and
	// delete resources that already exist.
	// +kubebuilder:default=true
	// +optional
	ProvisioningEnabled *bool `json:"provisioningEnabled,omitempty"`

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
	// spec.inputs. Every entry MUST carry a default unless it sets From.
	// +optional
	// +listType=map
	// +listMapKey=name
	Inputs []InputDeclaration `json:"inputs,omitempty"`

	// Profiles is the closed set of machine types this SCPC offers. An input
	// declared with From=profile resolves to the smallest profile whose cpu
	// and memory satisfy the SC's requested per-node resources (the larger of
	// the worker and control-plane pool asks). When no profile is large enough
	// the SimpleCluster is parked in Phase=NoHardware.
	// +optional
	// +listType=map
	// +listMapKey=name
	Profiles []Profile `json:"profiles,omitempty"`

	// Autoscaling declares that this SCPC's templates honor
	// .SC.workers.{min,max} and the underlying platform scales the worker
	// pool automatically (e.g. Gardener). When false, an SC requesting
	// autoscaling is provisioned at the fixed size .SC.workers.min and its
	// AutoscalingSupported condition is set to False.
	// +optional
	Autoscaling bool `json:"autoscaling,omitempty"`
}

// AllowsProvisioning reports whether the SCPC may create new target
// resources. Omission is intentionally treated as true for backwards
// compatibility with SCPCs created before the lifecycle gate existed.
func (s *SimpleClusterProviderConfigSpec) AllowsProvisioning() bool {
	return s.ProvisioningEnabled == nil || *s.ProvisioningEnabled
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
// +kubebuilder:resource:scope=Namespaced,shortName=scpc
// +kubebuilder:printcolumn:name="Reachable",type=boolean,JSONPath=`.status.reachable`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SimpleClusterProviderConfig is the namespaced admin-facing template/profile.
// A SimpleCluster resolves its providerConfigRefs in its own namespace first,
// then in the operator's fallback namespace (see --scpc-fallback-namespace).
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
