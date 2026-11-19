/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kindv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/kind-cluster-operator/api/v1alpha1"
)

// kindNamePattern mirrors kind's own cluster-name check: the name becomes a
// DNS-label-like docker container name prefix (<name>-control-plane).
var kindNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// nodeImagePattern mirrors the +kubebuilder:validation:Pattern marker on
// KindClusterSpec.NodeImage (kept in sync with both CRD copies by hand, the
// same way the rest of this operator's config is maintained).
var nodeImagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*(:[0-9]+)?(/[a-zA-Z0-9][a-zA-Z0-9._-]*)+(:[a-zA-Z0-9][a-zA-Z0-9._-]*)?(@sha256:[a-f0-9]{64})?$`)

func testKindCluster(namespace, name, clusterName string) *kindv1alpha1.KindCluster {
	return &kindv1alpha1.KindCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       kindv1alpha1.KindClusterSpec{ClusterName: clusterName},
	}
}

// TestEffectiveClusterName pins the F-KD-2 behavior: the docker name is the
// bare name by default (single-tenant compatibility) and namespace-scoped
// when PrefixClusterNameWithNamespace is on, so same-named KindClusters in
// different namespaces can no longer adopt or delete each other's clusters.
func TestEffectiveClusterName(t *testing.T) {
	cases := []struct {
		prefix bool
		kc     *kindv1alpha1.KindCluster
		want   string
	}{
		{false, testKindCluster("tenant-a", "demo", ""), "demo"},
		{false, testKindCluster("tenant-a", "demo", "demo-cluster"), "demo-cluster"},
		{true, testKindCluster("tenant-a", "demo", ""), "tenant-a-demo"},
		{true, testKindCluster("tenant-a", "demo", "demo-cluster"), "tenant-a-demo-cluster"},
	}
	for _, tc := range cases {
		r := &KindClusterReconciler{PrefixClusterNameWithNamespace: tc.prefix}
		if got := r.effectiveClusterName(tc.kc); got != tc.want {
			t.Errorf("prefix=%v %s/%s: got %q, want %q",
				tc.prefix, tc.kc.Namespace, tc.kc.Name, got, tc.want)
		}
	}
}

// TestEffectiveClusterNameCollisionIsNamespaceScoped is the two-tenant repro
// from the audit: both tenants name their SimpleCluster "demo".
func TestEffectiveClusterNameCollisionIsNamespaceScoped(t *testing.T) {
	a := testKindCluster("tenant-a", "demo", "demo-cluster")
	b := testKindCluster("tenant-b", "demo", "demo-cluster")

	plain := &KindClusterReconciler{}
	if plain.effectiveClusterName(a) != plain.effectiveClusterName(b) {
		t.Fatal("expected the unprefixed names to collide (documented default)")
	}

	scoped := &KindClusterReconciler{PrefixClusterNameWithNamespace: true}
	na, nb := scoped.effectiveClusterName(a), scoped.effectiveClusterName(b)
	if na == nb {
		t.Fatalf("prefixed names must differ, both are %q", na)
	}
	for _, n := range []string{na, nb} {
		// kind accepts the name and the derived container name stays valid.
		if !kindNamePattern.MatchString(n) {
			t.Errorf("%q is not a valid kind cluster name", n)
		}
		if len(n)+len("-control-plane") > 63 {
			t.Errorf("%q risks kind's \"probably too long\" warning", n)
		}
	}
}

// TestNodeImagePattern keeps the CRD pattern honest: the kindest/node shapes
// that work today must keep working, and arbitrary strings must not reach
// the host docker daemon (F-KD-1).
func TestNodeImagePattern(t *testing.T) {
	valid := []string{
		"kindest/node",
		"kindest/node:v1.33.1",
		"docker.io/kindest/node:v1.33.1",
		"registry.example.com:5000/kindest/node",
		"kindest/node@sha256:" + strings.Repeat("a", 64),
		"registry.example.com:5000/team/kindest/node:v1.33.1@sha256:" + strings.Repeat("0f", 32),
	}
	for _, s := range valid {
		if !nodeImagePattern.MatchString(s) {
			t.Errorf("%q should match the nodeImage pattern", s)
		}
	}

	invalid := []string{
		"",                  // empty means "field omitted"; SCPC omits it
		"latest",            // no registry/repo path
		"busybox latest",    // a space is not an image reference
		"kindest//node",     // empty path segment
		"kindest/node:",     // empty tag
		"{{ .Inputs.foo }}", // unrendered template noise
		"kindest/node:tag ", // trailing space
	}
	for _, s := range invalid {
		if nodeImagePattern.MatchString(s) {
			t.Errorf("%q should NOT match the nodeImage pattern", s)
		}
	}
}
