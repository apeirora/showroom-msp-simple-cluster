/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package kubeconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

// stubMapper implements apimeta.RESTMapper just enough for fetchSubResource:
// only RESTMapping is exercised.
type stubMapper struct {
	mapping *apimeta.RESTMapping
	err     error
}

func (m *stubMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*apimeta.RESTMapping, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.mapping, nil
}

func (m *stubMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*apimeta.RESTMapping, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []*apimeta.RESTMapping{m.mapping}, nil
}

func (m *stubMapper) KindFor(schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, fmt.Errorf("unused")
}
func (m *stubMapper) KindsFor(schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, fmt.Errorf("unused")
}
func (m *stubMapper) ResourceFor(schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, fmt.Errorf("unused")
}
func (m *stubMapper) ResourcesFor(schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, fmt.Errorf("unused")
}
func (m *stubMapper) ResourceSingularizer(string) (string, error) {
	return "", fmt.Errorf("unused")
}

func shootMapping() *apimeta.RESTMapping {
	return &apimeta.RESTMapping{
		Resource: schema.GroupVersionResource{
			Group:    "core.gardener.cloud",
			Version:  "v1beta1",
			Resource: "shoots",
		},
		GroupVersionKind: schema.GroupVersionKind{
			Group:   "core.gardener.cloud",
			Version: "v1beta1",
			Kind:    "Shoot",
		},
		Scope: apimeta.RESTScopeNamespace,
	}
}

func clusterScopedMapping() *apimeta.RESTMapping {
	return &apimeta.RESTMapping{
		Resource: schema.GroupVersionResource{
			Group:    "example.io",
			Version:  "v1",
			Resource: "clusters",
		},
		GroupVersionKind: schema.GroupVersionKind{
			Group:   "example.io",
			Version: "v1",
			Kind:    "Cluster",
		},
		Scope: apimeta.RESTScopeRoot,
	}
}

func sampleKubeconfigBytes() []byte {
	return []byte("apiVersion: v1\nkind: Config\nusers:\n- name: u\n  user:\n    token: opaque\n")
}

func TestFetchSubResource_GardenerShape(t *testing.T) {
	var capturedPath, capturedMethod string
	var capturedBody map[string]any

	expirationTimestamp := "2026-12-31T00:00:00Z"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		kc := sampleKubeconfigBytes()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"apiVersion": "authentication.gardener.cloud/v1alpha1",
			"kind":       "AdminKubeconfigRequest",
			"status": map[string]any{
				"kubeconfig":          base64.StdEncoding.EncodeToString(kc),
				"expirationTimestamp": expirationTimestamp,
			},
		})
	}))
	defer server.Close()

	tc := &target.Client{
		Mapper:     &stubMapper{mapping: shootMapping()},
		RESTConfig: &rest.Config{Host: server.URL},
	}
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{
		Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{
				ResourceName: "shoot",
				SubResource: &showroomv1alpha1.KubeconfigSubResourceSource{
					APIVersion:     "authentication.gardener.cloud/v1alpha1",
					Kind:           "AdminKubeconfigRequest",
					SubResource:    "adminkubeconfig",
					KubeconfigPath: "status.kubeconfig",
					ExpirationPath: "status.expirationTimestamp",
				},
				Validity: &metav1.Duration{Duration: 720 * time.Hour},
			},
		},
	}
	sc := &showroomv1alpha1.SimpleCluster{}
	resource := &showroomv1alpha1.ResourceSpec{
		Name:       "shoot",
		APIVersion: "core.gardener.cloud/v1beta1",
		Kind:       "Shoot",
	}
	live := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":      "myshoot",
			"namespace": "garden-sap",
		},
	}}

	kc, expiry, err := fetchSubResource(context.Background(), tc, scpc.Spec.KubeconfigSource.SubResource, scpc, sc, resource, live)
	if err != nil {
		t.Fatalf("fetchSubResource: %v", err)
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("method=%s want POST", capturedMethod)
	}
	wantPath := "/apis/core.gardener.cloud/v1beta1/namespaces/garden-sap/shoots/myshoot/adminkubeconfig"
	if capturedPath != wantPath {
		t.Errorf("path=%s want %s", capturedPath, wantPath)
	}
	if capturedBody["apiVersion"] != "authentication.gardener.cloud/v1alpha1" {
		t.Errorf("body apiVersion=%v want authentication.gardener.cloud/v1alpha1", capturedBody["apiVersion"])
	}
	if capturedBody["kind"] != "AdminKubeconfigRequest" {
		t.Errorf("body kind=%v", capturedBody["kind"])
	}
	spec, _ := capturedBody["spec"].(map[string]any)
	if spec == nil || spec["expirationSeconds"] == nil {
		t.Errorf("body spec.expirationSeconds missing, body=%+v", capturedBody)
	}
	// 720h = 2592000 seconds.
	if got, _ := spec["expirationSeconds"].(float64); int64(got) != int64((720 * time.Hour).Seconds()) {
		t.Errorf("expirationSeconds=%v want %d", spec["expirationSeconds"], int64((720 * time.Hour).Seconds()))
	}

	if string(kc) != string(sampleKubeconfigBytes()) {
		t.Errorf("kubeconfig bytes mismatch:\ngot %q\nwant %q", kc, sampleKubeconfigBytes())
	}
	if expiry == nil {
		t.Fatal("expected expiry, got nil")
	}
	want, _ := time.Parse(time.RFC3339, expirationTimestamp)
	if !expiry.Equal(want.UTC()) {
		t.Errorf("expiry=%v want %v", expiry, want)
	}
}

func TestFetchSubResource_ClusterScoped(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{"kubeconfig": base64.StdEncoding.EncodeToString(sampleKubeconfigBytes())},
		})
	}))
	defer server.Close()

	tc := &target.Client{
		Mapper:     &stubMapper{mapping: clusterScopedMapping()},
		RESTConfig: &rest.Config{Host: server.URL},
	}
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{
		Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{
				ResourceName: "cluster",
				SubResource: &showroomv1alpha1.KubeconfigSubResourceSource{
					APIVersion:     "x.example.io/v1",
					Kind:           "KubeconfigRequest",
					SubResource:    "kubeconfig",
					KubeconfigPath: "status.kubeconfig",
				},
			},
		},
	}
	sc := &showroomv1alpha1.SimpleCluster{}
	resource := &showroomv1alpha1.ResourceSpec{
		Name:       "cluster",
		APIVersion: "example.io/v1",
		Kind:       "Cluster",
	}
	live := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "mycluster"},
	}}

	_, expiry, err := fetchSubResource(context.Background(), tc, scpc.Spec.KubeconfigSource.SubResource, scpc, sc, resource, live)
	if err != nil {
		t.Fatalf("fetchSubResource: %v", err)
	}
	if expiry != nil {
		t.Errorf("expected nil expiry when ExpirationPath empty, got %v", expiry)
	}
	wantPath := "/apis/example.io/v1/clusters/mycluster/kubeconfig"
	if capturedPath != wantPath {
		t.Errorf("path=%s want %s", capturedPath, wantPath)
	}
}

func TestFetchSubResource_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden by RBAC"}`))
	}))
	defer server.Close()

	tc := &target.Client{
		Mapper:     &stubMapper{mapping: shootMapping()},
		RESTConfig: &rest.Config{Host: server.URL},
	}
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{
		Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
			KubeconfigSource: showroomv1alpha1.KubeconfigSource{
				ResourceName: "shoot",
				SubResource: &showroomv1alpha1.KubeconfigSubResourceSource{
					APIVersion:     "authentication.gardener.cloud/v1alpha1",
					Kind:           "AdminKubeconfigRequest",
					SubResource:    "adminkubeconfig",
					KubeconfigPath: "status.kubeconfig",
				},
			},
		},
	}
	live := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "x", "namespace": "ns"},
	}}
	_, _, err := fetchSubResource(context.Background(), tc, scpc.Spec.KubeconfigSource.SubResource,
		scpc, &showroomv1alpha1.SimpleCluster{},
		&showroomv1alpha1.ResourceSpec{APIVersion: "core.gardener.cloud/v1beta1", Kind: "Shoot"},
		live)
	if err == nil {
		t.Fatal("expected error from 403 response")
	}
	if !strings.Contains(err.Error(), "forbidden by RBAC") {
		t.Errorf("expected error to surface server body, got %v", err)
	}
}

func TestFetchSubResource_MissingKubeconfigPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":{"other":"value"}}`))
	}))
	defer server.Close()

	tc := &target.Client{
		Mapper:     &stubMapper{mapping: shootMapping()},
		RESTConfig: &rest.Config{Host: server.URL},
	}
	src := &showroomv1alpha1.KubeconfigSubResourceSource{
		APIVersion:     "v1",
		Kind:           "Req",
		SubResource:    "kubeconfig",
		KubeconfigPath: "status.kubeconfig",
	}
	live := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "x", "namespace": "ns"},
	}}
	_, _, err := fetchSubResource(context.Background(), tc, src,
		&showroomv1alpha1.SimpleClusterProviderConfig{}, &showroomv1alpha1.SimpleCluster{},
		&showroomv1alpha1.ResourceSpec{APIVersion: "core.gardener.cloud/v1beta1", Kind: "Shoot"},
		live)
	if err == nil || !strings.Contains(err.Error(), `kubeconfig not found at "status.kubeconfig"`) {
		t.Fatalf("expected kubeconfig-not-found error, got %v", err)
	}
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Now().UTC()

	cases := []struct {
		name string
		sc   showroomv1alpha1.SimpleCluster
		scpc showroomv1alpha1.SimpleClusterProviderConfig
		want bool
	}{
		{
			name: "Inline strategy: always refresh",
			scpc: showroomv1alpha1.SimpleClusterProviderConfig{
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						Inline: &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "x"},
					},
				},
			},
			sc: showroomv1alpha1.SimpleCluster{
				Status: showroomv1alpha1.SimpleClusterStatus{
					KubeconfigExpiresAt: &metav1.Time{Time: now.Add(48 * time.Hour)},
				},
			},
			want: true,
		},
		{
			name: "SecretRef strategy: always refresh",
			scpc: showroomv1alpha1.SimpleClusterProviderConfig{
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						SecretRef: &showroomv1alpha1.KubeconfigSecretSource{Name: "x"},
					},
				},
			},
			want: true,
		},
		{
			name: "SubResource: no expiry recorded yet",
			scpc: showroomv1alpha1.SimpleClusterProviderConfig{
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						SubResource: &showroomv1alpha1.KubeconfigSubResourceSource{},
					},
				},
			},
			want: true,
		},
		{
			name: "SubResource: expiry far outside renewal window",
			scpc: showroomv1alpha1.SimpleClusterProviderConfig{
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						SubResource:   &showroomv1alpha1.KubeconfigSubResourceSource{},
						Validity:      &metav1.Duration{Duration: 720 * time.Hour},
						RenewalBefore: &metav1.Duration{Duration: 72 * time.Hour},
					},
				},
			},
			sc: showroomv1alpha1.SimpleCluster{
				Status: showroomv1alpha1.SimpleClusterStatus{
					KubeconfigExpiresAt: &metav1.Time{Time: now.Add(200 * time.Hour)},
				},
			},
			want: false,
		},
		{
			name: "SubResource: expiry inside renewal window",
			scpc: showroomv1alpha1.SimpleClusterProviderConfig{
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						SubResource:   &showroomv1alpha1.KubeconfigSubResourceSource{},
						RenewalBefore: &metav1.Duration{Duration: 72 * time.Hour},
					},
				},
			},
			sc: showroomv1alpha1.SimpleCluster{
				Status: showroomv1alpha1.SimpleClusterStatus{
					KubeconfigExpiresAt: &metav1.Time{Time: now.Add(24 * time.Hour)},
				},
			},
			want: true,
		},
		{
			name: "SubResource: SC override beats SCPC default",
			scpc: showroomv1alpha1.SimpleClusterProviderConfig{
				Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
					KubeconfigSource: showroomv1alpha1.KubeconfigSource{
						SubResource:   &showroomv1alpha1.KubeconfigSubResourceSource{},
						RenewalBefore: &metav1.Duration{Duration: 1 * time.Hour},
					},
				},
			},
			sc: showroomv1alpha1.SimpleCluster{
				Spec: showroomv1alpha1.SimpleClusterSpec{
					KubeconfigRenewalBefore: &metav1.Duration{Duration: 72 * time.Hour},
				},
				Status: showroomv1alpha1.SimpleClusterStatus{
					KubeconfigExpiresAt: &metav1.Time{Time: now.Add(48 * time.Hour)},
				},
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NeedsRefresh(&tc.sc, &tc.scpc)
			if got != tc.want {
				t.Errorf("NeedsRefresh=%v want %v", got, tc.want)
			}
		})
	}
}
