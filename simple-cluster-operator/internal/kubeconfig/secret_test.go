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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func ensureSecretFixtures(t *testing.T) (*runtime.Scheme, *showroomv1alpha1.SimpleCluster, *showroomv1alpha1.SimpleClusterProviderConfig) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := showroomv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add showroom scheme: %v", err)
	}
	sc := &showroomv1alpha1.SimpleCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default", UID: "sc-uid"},
	}
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "provider"},
	}
	return scheme, sc, scpc
}

func TestEnsureSecretLabelsNewSecret(t *testing.T) {
	scheme, sc, scpc := ensureSecretFixtures(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	res, err := EnsureSecret(context.Background(), c, scheme, sc, scpc, []byte("kubeconfig-bytes"), nil)
	if err != nil {
		t.Fatalf("EnsureSecret: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed=true on create")
	}

	var secret corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "demo" + SecretSuffix, Namespace: "default"}, &secret); err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if secret.Labels[LabelKubeconfig] != "true" {
		t.Fatalf("expected label %s=true, got labels %v", LabelKubeconfig, secret.Labels)
	}
}

func TestEnsureSecretRelabelsExistingSecret(t *testing.T) {
	scheme, sc, scpc := ensureSecretFixtures(t)
	// Pre-existing Secret from an older operator version: same data, no label.
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "demo" + SecretSuffix, Namespace: "default"},
		Data:       map[string][]byte{SecretDataKey: []byte("kubeconfig-bytes")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	res, err := EnsureSecret(context.Background(), c, scheme, sc, scpc, []byte("kubeconfig-bytes"), nil)
	if err != nil {
		t.Fatalf("EnsureSecret: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed=true when adding the missing label")
	}

	var secret corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "demo" + SecretSuffix, Namespace: "default"}, &secret); err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if secret.Labels[LabelKubeconfig] != "true" {
		t.Fatalf("expected label %s=true, got labels %v", LabelKubeconfig, secret.Labels)
	}
}

func TestEnsureSecretUnchangedWhenDataAndLabelMatch(t *testing.T) {
	scheme, sc, scpc := ensureSecretFixtures(t)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo" + SecretSuffix,
			Namespace: "default",
			Labels:    map[string]string{LabelKubeconfig: "true"},
		},
		Data: map[string][]byte{SecretDataKey: []byte("kubeconfig-bytes")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	res, err := EnsureSecret(context.Background(), c, scheme, sc, scpc, []byte("kubeconfig-bytes"), nil)
	if err != nil {
		t.Fatalf("EnsureSecret: %v", err)
	}
	if res.Changed {
		t.Fatal("expected Changed=false for an up-to-date Secret")
	}
}
