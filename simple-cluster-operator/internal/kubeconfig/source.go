/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package kubeconfig fetches NC's kubeconfig from a target resource R and
// mirrors it into a Secret in SC.namespace. See DESIGN.md §6.
//
// Three strategies, all implemented:
//
//   - inline:      walk a JSONPath into R and read kubeconfig bytes.
//   - secretRef:   render the Secret name/namespace as Go templates, check
//     its metadata requirements, then GET its data from the target cluster.
//   - subResource: POST a Kind body to R's subresource URL (e.g.
//     Gardener's adminkubeconfig) and read kubeconfig +
//     expiry from the response. See subresource.go.
package kubeconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/pathwalk"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
	rendertpl "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/template"
)

// Fetch resolves SCPC.KubeconfigSource against the named resource's live R
// on the target cluster. Returns the kubeconfig bytes plus an optional
// expiry hint published by the source (SubResource reports its own
// expiration; Inline/SecretRef do not, so expiry comes from inspecting
// the bytes via TokenTimes).
func Fetch(
	ctx context.Context,
	tc *target.Client,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	live *unstructured.Unstructured,
) ([]byte, *time.Time, error) {
	ks := &scpc.Spec.KubeconfigSource
	switch {
	case ks.Inline != nil:
		return fetchInline(ks.Inline, live)
	case ks.SecretRef != nil:
		return fetchSecretRef(ctx, tc, ks.SecretRef, scpc, sc, resource, live)
	case ks.SubResource != nil:
		return fetchSubResource(ctx, tc, ks.SubResource, scpc, sc, resource, live)
	}
	return nil, nil, fmt.Errorf("kubeconfigSource has no strategy set")
}

func fetchInline(src *showroomv1alpha1.KubeconfigInlineSource, live *unstructured.Unstructured) ([]byte, *time.Time, error) {
	raw, ok := pathwalk.Walk(live.Object, src.KubeconfigPath)
	if !ok {
		return nil, nil, fmt.Errorf("inline.kubeconfigPath %q not found in live object", src.KubeconfigPath)
	}
	data, err := coerceBytes(raw)
	if err != nil {
		return nil, nil, err
	}
	return data, nil, nil
}

func fetchSecretRef(
	ctx context.Context,
	tc *target.Client,
	src *showroomv1alpha1.KubeconfigSecretSource,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	live *unstructured.Unstructured,
) ([]byte, *time.Time, error) {
	renderCtx, err := rendertpl.BuildContext(scpc, sc, resource,
		map[string]rendertpl.Predecessor{resource.Name: {Objects: []*unstructured.Unstructured{live}}})
	if err != nil {
		return nil, nil, fmt.Errorf("build template context: %w", err)
	}

	name, err := rendertpl.RenderString("secretRef.name", src.Name, renderCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("render secretRef.name: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, fmt.Errorf("rendered secretRef.name is empty")
	}

	ns := src.Namespace
	if ns != "" {
		ns, err = rendertpl.RenderString("secretRef.namespace", ns, renderCtx)
		if err != nil {
			return nil, nil, fmt.Errorf("render secretRef.namespace: %w", err)
		}
		ns = strings.TrimSpace(ns)
	}
	if ns == "" {
		ns = live.GetNamespace()
	}

	dataKey := src.DataKey
	if dataKey == "" {
		dataKey = "kubeconfig"
	}

	secretGVR := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	if tc.Metadata == nil {
		return nil, nil, fmt.Errorf("target client has no metadata reader")
	}
	requirements, err := renderSecretMetadataRequirements(src, renderCtx)
	if err != nil {
		return nil, nil, err
	}
	var labelSelector string
	if len(requirements.labels) > 0 {
		selector, selectorErr := labels.Set(requirements.labels).AsValidatedSelector()
		if selectorErr != nil {
			return nil, nil, fmt.Errorf("build required Secret label selector: %w", selectorErr)
		}
		labelSelector = selector.String()
	}
	metadataObj, err := tc.Metadata.Resource(secretGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("get Secret %s/%s metadata on target cluster: %w", ns, name, err)
	}
	if metadataObj.GetUID() == "" || metadataObj.GetResourceVersion() == "" {
		return nil, nil, fmt.Errorf("secret %s/%s metadata response has no UID or resourceVersion", ns, name)
	}
	if err := validateRenderedSecretMetadataRequirements(metadataObj, requirements); err != nil {
		return nil, nil, fmt.Errorf("validate Secret %s/%s metadata requirements: %w", ns, name, err)
	}

	var obj *unstructured.Unstructured
	if labelSelector == "" {
		// Backward compatibility for legacy and annotation-only SecretRefs. A
		// name-only GET cannot prevent a raced same-name replacement's bytes
		// from crossing the transport, so UID/RV below is a use boundary only.
		obj, err = tc.Dynamic.Resource(secretGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, fmt.Errorf("get Secret %s/%s data on target cluster: %w", ns, name, err)
		}
	} else {
		// Put every positive predicate supported by the Secret API on the
		// server-side data request. A replacement that has lost any required
		// label is filtered before its data is serialized to this controller.
		list, listErr := tc.Dynamic.Resource(secretGVR).Namespace(ns).List(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", name).String(),
			LabelSelector: labelSelector,
			Limit:         2,
		})
		if listErr != nil {
			return nil, nil, fmt.Errorf("list Secret %s/%s data with required labels on target cluster: %w", ns, name, listErr)
		}
		if len(list.Items) != 1 {
			return nil, nil, fmt.Errorf(
				"secret %s/%s no longer matches all required labels or is absent; data query returned %d objects; retry",
				ns, name, len(list.Items))
		}
		obj = &list.Items[0]
		if obj.GetName() != name || obj.GetNamespace() != ns {
			return nil, nil, fmt.Errorf(
				"secret data query returned unexpected object %s/%s, want %s/%s",
				obj.GetNamespace(), obj.GetName(), ns, name)
		}
		if err := validateRenderedSecretMetadataRequirements(obj, requirements); err != nil {
			return nil, nil, fmt.Errorf("validate listed Secret %s/%s metadata requirements: %w", ns, name, err)
		}
	}
	if obj.GetUID() != metadataObj.GetUID() || obj.GetResourceVersion() != metadataObj.GetResourceVersion() {
		return nil, nil, fmt.Errorf("secret %s/%s changed after its metadata requirements were checked; retry", ns, name)
	}

	// Secret data fields are base64-encoded strings in unstructured.
	encoded, ok, err := unstructured.NestedString(obj.Object, "data", dataKey)
	if err != nil {
		return nil, nil, fmt.Errorf("read Secret %s/%s data.%s: %w", ns, name, dataKey, err)
	}
	if !ok {
		return nil, nil, fmt.Errorf("secret %s/%s missing data.%s", ns, name, dataKey)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("secret %s/%s data.%s not valid base64: %w", ns, name, dataKey, err)
	}
	return decoded, nil, nil
}

type renderedSecretMetadataRequirements struct {
	labels      map[string]string
	annotations map[string]string
}

func renderSecretMetadataRequirements(
	src *showroomv1alpha1.KubeconfigSecretSource,
	renderCtx map[string]any,
) (renderedSecretMetadataRequirements, error) {
	result := renderedSecretMetadataRequirements{
		labels:      make(map[string]string, len(src.RequiredLabels)),
		annotations: make(map[string]string, len(src.RequiredAnnotations)),
	}
	checks := []struct {
		kind string
		want map[string]string
		out  map[string]string
	}{
		{kind: "label", want: src.RequiredLabels, out: result.labels},
		{kind: "annotation", want: src.RequiredAnnotations, out: result.annotations},
	}
	for _, check := range checks {
		keys := make([]string, 0, len(check.want))
		for key := range check.want {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if strings.TrimSpace(key) == "" {
				return renderedSecretMetadataRequirements{}, fmt.Errorf("required %s key is empty", check.kind)
			}
			expected, err := rendertpl.RenderString(
				"secretRef.requiredMetadata."+check.kind+"."+key,
				check.want[key], renderCtx,
			)
			if err != nil {
				return renderedSecretMetadataRequirements{}, fmt.Errorf("render required %s %q: %w", check.kind, key, err)
			}
			check.out[key] = expected
		}
	}
	return result, nil
}

// validateSecretMetadataRequirements enforces the provider config's exact metadata
// requirements before kubeconfig bytes are fetched. A status field may legitimately supply
// a Secret name, but a name alone cannot distinguish an expected transport copy from an
// unrelated local collision. Both maps are optional for backward compatibility.
//
// Metadata equality is not issuer authentication. A provider using these requirements must
// protect the required keys with admission/RBAC or publish a stronger claim-to-Secret
// relationship outside the generic SimpleCluster core. At least one required label also
// lets the data request carry a server-side selector; annotation-only requirements remain
// a credential-use boundary, not a read-exposure boundary.
func validateSecretMetadataRequirements(
	obj metav1.Object,
	src *showroomv1alpha1.KubeconfigSecretSource,
	renderCtx map[string]any,
) error {
	requirements, err := renderSecretMetadataRequirements(src, renderCtx)
	if err != nil {
		return err
	}
	return validateRenderedSecretMetadataRequirements(obj, requirements)
}

func validateRenderedSecretMetadataRequirements(
	obj metav1.Object,
	requirements renderedSecretMetadataRequirements,
) error {
	checks := []struct {
		kind   string
		want   map[string]string
		actual map[string]string
	}{
		{kind: "label", want: requirements.labels, actual: obj.GetLabels()},
		{kind: "annotation", want: requirements.annotations, actual: obj.GetAnnotations()},
	}
	for _, check := range checks {
		keys := make([]string, 0, len(check.want))
		for key := range check.want {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if actual, ok := check.actual[key]; !ok || actual != check.want[key] {
				return fmt.Errorf("required %s %q does not match", check.kind, key)
			}
		}
	}
	return nil
}

// coerceBytes interprets a JSONPath-extracted value as kubeconfig bytes.
// Accepts a plain YAML string or a base64-encoded YAML string.
func coerceBytes(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("kubeconfig field is not a string (got %T)", v)
	}
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil && bytes.Contains(decoded, []byte("apiVersion")) {
		return decoded, nil
	}
	if strings.Contains(s, "apiVersion") {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("kubeconfig content does not look like a kubeconfig (no apiVersion)")
}
