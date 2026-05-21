/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package kubeconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/pathwalk"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

// fetchSubResource POSTs a Kind body (typically with spec.expirationSeconds)
// to R's subresource URL on the target cluster and extracts kubeconfig
// bytes + optional expiry from the response.
//
// The URL is built per the standard k8s shape:
//
//	/api/<v>/[namespaces/<ns>/]<plural>/<name>/<subResource>           (core group)
//	/apis/<g>/<v>/[namespaces/<ns>/]<plural>/<name>/<subResource>      (other groups)
//
// dynamic.Interface does not expose arbitrary subresource POST, so we
// reach for net/http via rest.HTTPClientFor — it inherits auth, TLS, and
// impersonation from the target RESTConfig.
func fetchSubResource(
	ctx context.Context,
	tc *target.Client,
	src *showroomv1alpha1.KubeconfigSubResourceSource,
	scpc *showroomv1alpha1.SimpleClusterProviderConfig,
	sc *showroomv1alpha1.SimpleCluster,
	resource *showroomv1alpha1.ResourceSpec,
	live *unstructured.Unstructured,
) ([]byte, *time.Time, error) {
	if tc.RESTConfig == nil {
		return nil, nil, fmt.Errorf("subResource strategy requires a non-nil target REST config")
	}

	parentGV, err := schema.ParseGroupVersion(resource.APIVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("parse parent apiVersion %q: %w", resource.APIVersion, err)
	}
	parentGVK := parentGV.WithKind(resource.Kind)
	mapping, err := tc.Mapper.RESTMapping(parentGVK.GroupKind(), parentGVK.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("RESTMapping %s: %w", parentGVK, err)
	}

	body := map[string]any{
		"apiVersion": src.APIVersion,
		"kind":       src.Kind,
		"spec":       map[string]any{},
	}
	if validity := pickValidity(scpc, sc); validity > 0 {
		body["spec"].(map[string]any)["expirationSeconds"] = int64(validity.Seconds())
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal subresource body: %w", err)
	}

	url := buildSubResourceURL(tc.RESTConfig.Host, parentGV, mapping, live, src.SubResource)

	httpClient, err := rest.HTTPClientFor(tc.RESTConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("build HTTP client for target cluster: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("build subresource request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read subresource response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("subresource POST %s: %s: %s",
			url, resp.Status, strings.TrimSpace(string(raw)))
	}

	var response map[string]any
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, nil, fmt.Errorf("parse subresource response: %w", err)
	}

	kcVal, ok := pathwalk.Walk(response, src.KubeconfigPath)
	if !ok {
		return nil, nil, fmt.Errorf("kubeconfig not found at %q in subresource response", src.KubeconfigPath)
	}
	kcBytes, err := coerceBytes(kcVal)
	if err != nil {
		return nil, nil, fmt.Errorf("kubeconfig at %q: %w", src.KubeconfigPath, err)
	}

	var expiresAt *time.Time
	if src.ExpirationPath != "" {
		if expVal, ok := pathwalk.Walk(response, src.ExpirationPath); ok {
			if expStr, ok := expVal.(string); ok && expStr != "" {
				if t, perr := time.Parse(time.RFC3339, expStr); perr == nil {
					t = t.UTC()
					expiresAt = &t
				}
			}
		}
	}
	return kcBytes, expiresAt, nil
}

func buildSubResourceURL(host string, gv schema.GroupVersion, mapping *apimeta.RESTMapping, live *unstructured.Unstructured, subResource string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(host, "/"))
	if gv.Group == "" {
		b.WriteString("/api/")
		b.WriteString(gv.Version)
	} else {
		b.WriteString("/apis/")
		b.WriteString(gv.Group)
		b.WriteString("/")
		b.WriteString(gv.Version)
	}
	if mapping.Scope.Name() == apimeta.RESTScopeNameNamespace {
		b.WriteString("/namespaces/")
		b.WriteString(live.GetNamespace())
	}
	b.WriteString("/")
	b.WriteString(mapping.Resource.Resource)
	b.WriteString("/")
	b.WriteString(live.GetName())
	b.WriteString("/")
	b.WriteString(subResource)
	return b.String()
}

// pickValidity returns the kubeconfig validity in priority order:
// SC.spec override → SCPC.spec.kubeconfigSource.validity → 0 (unset).
func pickValidity(scpc *showroomv1alpha1.SimpleClusterProviderConfig, sc *showroomv1alpha1.SimpleCluster) time.Duration {
	if sc.Spec.KubeconfigValidity != nil {
		return sc.Spec.KubeconfigValidity.Duration
	}
	if scpc.Spec.KubeconfigSource.Validity != nil {
		return scpc.Spec.KubeconfigSource.Validity.Duration
	}
	return 0
}

// pickRenewBefore returns how long before expiry to renew, in priority
// order: SC.spec override → SCPC.spec.kubeconfigSource.renewalBefore →
// validity/5 (min 1h) → 0 (no implicit renewal, only renew once expired).
func pickRenewBefore(scpc *showroomv1alpha1.SimpleClusterProviderConfig, sc *showroomv1alpha1.SimpleCluster) time.Duration {
	if sc.Spec.KubeconfigRenewalBefore != nil {
		return sc.Spec.KubeconfigRenewalBefore.Duration
	}
	if scpc.Spec.KubeconfigSource.RenewalBefore != nil {
		return scpc.Spec.KubeconfigSource.RenewalBefore.Duration
	}
	validity := pickValidity(scpc, sc)
	if validity == 0 {
		return 0
	}
	rb := validity / 5
	if rb < time.Hour {
		rb = time.Hour
	}
	return rb
}

// NeedsRefresh reports whether the kubeconfig stored on SC.Status should
// be re-fetched. For Inline and SecretRef strategies this is always true
// (the fetch is a cheap read and upstream may have rotated). For
// SubResource the renewalBefore window gates the POST so we don't issue
// a fresh credential every reconcile.
func NeedsRefresh(sc *showroomv1alpha1.SimpleCluster, scpc *showroomv1alpha1.SimpleClusterProviderConfig) bool {
	if scpc.Spec.KubeconfigSource.SubResource == nil {
		return true
	}
	if sc.Status.KubeconfigExpiresAt == nil || sc.Status.KubeconfigExpiresAt.Time.IsZero() {
		return true
	}
	renewBefore := pickRenewBefore(scpc, sc)
	now := time.Now().UTC()
	if renewBefore == 0 {
		return now.After(sc.Status.KubeconfigExpiresAt.Time)
	}
	return now.Add(renewBefore).After(sc.Status.KubeconfigExpiresAt.Time)
}
