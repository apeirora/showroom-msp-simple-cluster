package controller

import (
	"context"
	"testing"

	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/index"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testConfig() Config {
	return Config{
		ClusterLabelKey:      "kubernetes.io/metadata.name",
		ExportsAnnotationKey: "bind.simple-cluster.apeirora.eu/exports",
		Separator:            ",",
		ProviderSeparator:    "#",
		CreateNamespaces:     true,
		ManagedLabelKey:      "bind-watcher.apeirora.eu/managed",
	}
}

// A bound cluster with no namespace gets one created, marked and annotated.
func TestSyncCreatesMarkedNamespace(t *testing.T) {
	msp := fake.NewClientBuilder().WithScheme(Scheme).Build()
	idx := index.New()
	idx.Set("X", "kcp-0#X", "b1", "simplecluster")
	idx.Set("X", "kcp-1#X", "b2", "dati")
	c := New(context.Background(), nil, msp, idx, testConfig())

	if err := c.syncNamespaceAnnotation(context.Background(), "X"); err != nil {
		t.Fatal(err)
	}

	ns := &corev1.Namespace{}
	if err := msp.Get(context.Background(), client.ObjectKey{Name: "X"}, ns); err != nil {
		t.Fatalf("namespace X not created: %v", err)
	}
	if ns.Labels[testConfig().ManagedLabelKey] != "true" {
		t.Errorf("marker label missing: %v", ns.Labels)
	}
	if got, want := ns.Annotations[testConfig().ExportsAnnotationKey], "dati,simplecluster"; got != want {
		t.Errorf("exports annotation = %q, want %q", got, want)
	}
}

// A cluster with no bound exports and no namespace must not be created.
func TestSyncNoCreateWhenNoExports(t *testing.T) {
	msp := fake.NewClientBuilder().WithScheme(Scheme).Build()
	c := New(context.Background(), nil, msp, index.New(), testConfig())

	if err := c.syncNamespaceAnnotation(context.Background(), "Y"); err != nil {
		t.Fatal(err)
	}
	if err := msp.Get(context.Background(), client.ObjectKey{Name: "Y"}, &corev1.Namespace{}); err == nil {
		t.Fatal("namespace Y should not have been created")
	}
}

// With creation disabled, a bound-but-namespaceless cluster is left alone.
func TestSyncNoCreateWhenDisabled(t *testing.T) {
	msp := fake.NewClientBuilder().WithScheme(Scheme).Build()
	idx := index.New()
	idx.Set("Z", "kcp-0#Z", "b1", "simplecluster")
	cfg := testConfig()
	cfg.CreateNamespaces = false
	c := New(context.Background(), nil, msp, idx, cfg)

	if err := c.syncNamespaceAnnotation(context.Background(), "Z"); err != nil {
		t.Fatal(err)
	}
	if err := msp.Get(context.Background(), client.ObjectKey{Name: "Z"}, &corev1.Namespace{}); err == nil {
		t.Fatal("namespace Z should not have been created when creation disabled")
	}
}
