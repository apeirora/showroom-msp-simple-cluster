package target

import (
	"context"
	"fmt"
	"slices"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const bindingAPIGroup = "apis.kcp.io"

// AccountAuthorization pins the API identity and permission selectors for one resource.
// Resolve validates the graph contract; CheckObject validates the complete object before a write.
type AccountAuthorization struct {
	Mapping      *apimeta.RESTMapping
	IdentityHash string
	selectors    []permissionScope
}

type permissionScope struct {
	all       bool
	selector  labels.Selector
	resources []map[string]any
}

// CheckObject requires both the current accepted and applied permission selectors.
func (a *AccountAuthorization) CheckObject(obj *unstructured.Unstructured) error {
	if obj.GroupVersionKind() != a.Mapping.GroupVersionKind {
		return fmt.Errorf("account object GVK differs from the authorized API")
	}
	if a.Mapping.Scope.Name() == apimeta.RESTScopeNameRoot && obj.GetNamespace() != "" {
		return fmt.Errorf("cluster-scoped account object has a namespace")
	}
	if a.Mapping.Scope.Name() == apimeta.RESTScopeNameNamespace && obj.GetNamespace() == "" {
		return fmt.Errorf("namespaced account object has no namespace")
	}
	for _, scope := range a.selectors {
		if scope.all {
			continue
		}
		if scope.selector != nil && scope.selector.Matches(labels.Set(obj.GetLabels())) {
			continue
		}
		matched := false
		for _, resource := range scope.resources {
			name, _ := resource["name"].(string)
			namespace, _ := resource["namespace"].(string)
			if (name != "" || namespace != "") && (name == "" || name == obj.GetName()) && (namespace == "" || namespace == obj.GetNamespace()) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("account object does not match an accepted and applied permission selector")
		}
	}
	return nil
}

// ResolveAccountResource validates binding, identity, requested verbs, acceptance and application.
// It performs only reads. The API server still enforces authorization on every actual operation.
func ResolveAccountResource(ctx context.Context, dyn dynamic.Interface, mapper apimeta.RESTMapper,
	simpleClusterIdentity string, gvk schema.GroupVersionKind, verbs ...string) (*AccountAuthorization, error) {
	if simpleClusterIdentity == "" || len(verbs) == 0 {
		return nil, fmt.Errorf("account authorization requires the provider API identity and verbs")
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve account child API: %w", err)
	}
	bindingMapping, err := mapper.RESTMapping(schema.GroupKind{Group: bindingAPIGroup, Kind: "APIBinding"})
	if err != nil {
		return nil, fmt.Errorf("account APIBinding metadata is unavailable: %w", err)
	}
	bindings, err := dyn.Resource(bindingMapping.Resource).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("read account APIBindings: %w", err)
	}
	providerBinding, childIdentity, err := boundAPIIdentities(bindings.Items, simpleClusterIdentity, mapping.Resource)
	if err != nil {
		return nil, err
	}
	// Resources exported by SimpleCluster itself need no cross-export permission claim.
	if childIdentity == simpleClusterIdentity {
		return &AccountAuthorization{Mapping: mapping, IdentityHash: childIdentity}, nil
	}
	legacy := bindingMapping.Resource.Version == legacyBindingVersion
	result := &AccountAuthorization{Mapping: mapping, IdentityHash: childIdentity}
	for _, source := range []struct {
		path             []string
		accepted, scoped bool
	}{
		{[]string{"status", "exportPermissionClaims"}, false, false},
		{[]string{"spec", "permissionClaims"}, true, true},
		{[]string{"status", "appliedPermissionClaims"}, false, true},
	} {
		found := false
		for _, claim := range mapsAt(providerBinding.Object, source.path...) {
			group, _ := claim["group"].(string)
			if group != mapping.Resource.Group || claim["resource"] != mapping.Resource.Resource {
				continue
			}
			identity, _ := claim["identityHash"].(string)
			if identity != childIdentity {
				continue
			}
			if source.accepted && claim["state"] != "Accepted" {
				return nil, fmt.Errorf("account child permission claim is not Accepted")
			}
			if found {
				return nil, fmt.Errorf("ambiguous account child permission claim")
			}
			if err := claimVerbs(claim, verbs, legacy); err != nil {
				return nil, err
			}
			scope, err := claimScope(claim, source.scoped, legacy)
			if err != nil {
				return nil, err
			}
			if source.scoped {
				result.selectors = append(result.selectors, scope)
			}
			found = true
		}
		if !found {
			return nil, fmt.Errorf("account child permission claim is missing from %v", source.path)
		}
	}
	return result, nil
}

func boundAPIIdentities(bindings []unstructured.Unstructured, simpleClusterIdentity string, wanted schema.GroupVersionResource) (*unstructured.Unstructured, string, error) {
	var providerBinding *unstructured.Unstructured
	childIdentity := ""
	childFound := wanted.Group == ""
	for i := range bindings {
		binding := &bindings[i]
		if !bindingBound(binding) {
			continue
		}
		for _, bound := range mapsAt(binding.Object, "status", "boundResources") {
			group, _ := bound["group"].(string)
			resource, _ := bound["resource"].(string)
			identity, _, _ := unstructured.NestedString(bound, "schema", "identityHash")
			if group == simpleClusters.Group && resource == simpleClusters.Resource && identity == simpleClusterIdentity {
				if providerBinding != nil {
					return nil, "", fmt.Errorf("ambiguous SimpleCluster account APIBinding")
				}
				providerBinding = binding
			}
			if group == wanted.Group && resource == wanted.Resource {
				if identity == "" {
					return nil, "", fmt.Errorf("bound child API has no identity")
				}
				if childFound && childIdentity != identity {
					return nil, "", fmt.Errorf("ambiguous account child API identity")
				}
				childFound, childIdentity = true, identity
			}
		}
	}
	if providerBinding == nil {
		return nil, "", fmt.Errorf("the owning SimpleCluster APIBinding is not Bound")
	}
	if !childFound {
		return nil, "", fmt.Errorf("the child API has no Bound account APIBinding")
	}

	return providerBinding, childIdentity, nil
}

func bindingBound(binding *unstructured.Unstructured) bool {
	phase, _, _ := unstructured.NestedString(binding.Object, "status", "phase")
	if phase != "Bound" || binding.GetUID() == "" || !binding.GetDeletionTimestamp().IsZero() {
		return false
	}
	initial, current := false, false
	for _, condition := range mapsAt(binding.Object, "status", "conditions") {
		switch condition["type"] {
		case "InitialBindingCompleted":
			initial = condition["status"] == string(metav1.ConditionTrue)
		case "BindingUpToDate":
			current = condition["status"] == string(metav1.ConditionTrue)
		case "Ready", "APIExportValid":
			if condition["status"] != string(metav1.ConditionTrue) {
				return false
			}
		}
	}
	return initial && current
}

func mapsAt(obj map[string]any, path ...string) []map[string]any {
	items, _, _ := unstructured.NestedSlice(obj, path...)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if value, ok := item.(map[string]any); ok {
			result = append(result, value)
		}
	}
	return result
}

func claimVerbs(claim map[string]any, required []string, legacy bool) error {
	values, found, err := unstructured.NestedStringSlice(claim, "verbs")
	if err != nil {
		return fmt.Errorf("invalid account permission verbs")
	}
	// v1alpha1 claims have no verb field and grant the full legacy claim.
	if !found && legacy {
		if _, found := claim["all"]; found {
			return nil
		}
		if _, found := claim["resourceSelector"]; found {
			return nil
		}
	}
	for _, verb := range required {
		if !slices.Contains(values, "*") && !slices.Contains(values, verb) {
			return fmt.Errorf("account permission claim does not grant %s", verb)
		}
	}
	return nil
}

func claimScope(claim map[string]any, required, legacy bool) (permissionScope, error) {
	if selector, found, err := unstructured.NestedMap(claim, "selector"); err != nil {
		return permissionScope{}, fmt.Errorf("invalid permission selector")
	} else if found {
		all, _ := selector["matchAll"].(bool)
		if all {
			if len(selector) != 1 {
				return permissionScope{}, fmt.Errorf("matchAll cannot combine with permission label selectors")
			}
			return permissionScope{all: true}, nil
		}
		for key := range selector {
			if key != "matchLabels" && key != "matchExpressions" && key != "matchAll" {
				return permissionScope{}, fmt.Errorf("unknown account permission selector field")
			}
		}
		matchLabels, _, labelsErr := unstructured.NestedStringMap(selector, "matchLabels")
		matchExpressions, _, expressionsErr := unstructured.NestedSlice(selector, "matchExpressions")
		if labelsErr != nil || expressionsErr != nil || (len(matchLabels) == 0 && len(matchExpressions) == 0) {
			return permissionScope{}, fmt.Errorf("empty or invalid account permission selector")
		}
		var labelSelector metav1.LabelSelector
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(selector, &labelSelector); err != nil {
			return permissionScope{}, fmt.Errorf("invalid account label selector")
		}
		parsed, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return permissionScope{}, fmt.Errorf("invalid account label selector: %w", err)
		}
		return permissionScope{selector: parsed}, nil
	}
	if all, _ := claim["all"].(bool); legacy && all {
		return permissionScope{all: true}, nil
	}
	if resources := mapsAt(claim, "resourceSelector"); legacy && len(resources) > 0 {
		return permissionScope{resources: resources}, nil
	}
	if !required {
		return permissionScope{all: true}, nil
	}
	return permissionScope{}, fmt.Errorf("account permission claim has no scope")
}
