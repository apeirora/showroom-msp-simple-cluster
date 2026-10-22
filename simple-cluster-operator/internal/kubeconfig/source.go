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
//   - secretRef:   render the Secret name/namespace as Go templates and
//     GET the Secret from the target cluster.
//   - subResource: POST a Kind body to R's subresource URL (e.g.
//     Gardener's adminkubeconfig) and read kubeconfig +
//     expiry from the response. See subresource.go.
package kubeconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/pathwalk"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
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
		if live.GetAPIVersion() == "v1" && live.GetKind() == "Secret" {
			if _, err := tc.SecretPolicy.Resolve(sourceOwnerNamespace(tc, sc), live.GetNamespace(), live.GetName(), secretpolicy.Copy); err != nil {
				return nil, nil, err
			}
		}
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
		ns = sourceOwnerNamespace(tc, sc)
	}

	dataKey := src.DataKey
	if dataKey == "" {
		dataKey = "kubeconfig"
	}

	ns, err = tc.SecretPolicy.Resolve(sourceOwnerNamespace(tc, sc), ns, name, secretpolicy.Copy)
	if err != nil {
		return nil, nil, err
	}
	secretGVR := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	obj, err := tc.Dynamic.Resource(secretGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("get Secret %s/%s on target cluster: %w", ns, name, err)
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

// sourceOwnerNamespace separates the account destination from the physical parent projection.
func sourceOwnerNamespace(tc *target.Client, sc *showroomv1alpha1.SimpleCluster) string {
	if tc.AccountParent != nil {
		return tc.OwnerNamespace
	}
	return sc.Namespace
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
