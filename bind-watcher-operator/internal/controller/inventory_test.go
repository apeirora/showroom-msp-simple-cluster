package controller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/index"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type snapshotInventory struct {
	bindings []apisv1alpha1.APIBinding
	err      error
	called   chan struct{}
}

func (s *snapshotInventory) WithSnapshot(_ context.Context, publish func([]apisv1alpha1.APIBinding) error) error {
	if s.called != nil {
		defer close(s.called)
	}
	if s.err != nil {
		return s.err
	}
	return publish(s.bindings)
}

func binding(cluster, name, export string) apisv1alpha1.APIBinding {
	return apisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{"kcp.io/cluster": cluster}},
		Spec:       apisv1alpha1.APIBindingSpec{Reference: apisv1alpha1.BindingReference{Export: &apisv1alpha1.ExportBindingReference{Name: export}}},
	}
}

func annotation(t *testing.T, msp client.Client) string {
	t.Helper()
	ns := &corev1.Namespace{}
	if err := msp.Get(context.Background(), client.ObjectKey{Name: "account"}, ns); err != nil {
		t.Fatal(err)
	}
	return ns.Annotations[testConfig().ExportsAnnotationKey]
}

func existingNamespace() *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "account", Labels: map[string]string{testConfig().ClusterLabelKey: "account"},
		Annotations: map[string]string{testConfig().ExportsAnnotationKey: "arsys,sap.eu"}}}
}

func TestStartupNeverPublishesPartialIndex(t *testing.T) {
	ctx := context.Background()
	msp := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(existingNamespace()).Build()
	idx := index.New()
	// This was the incident: arsys's first Add reached the workqueue before
	// the sap provider's cache was populated, despite a complete old annotation.
	idx.Set("account", "arsys#account", "arsys", "arsys")
	c := New(ctx, nil, msp, idx, testConfig())
	snapshot := &snapshotInventory{bindings: []apisv1alpha1.APIBinding{binding("account", "arsys", "arsys")}, err: errors.New("sap initial cache not synchronized")}
	c.Inventory = snapshot
	if _, err := c.Reconcile(ctx, mcreconcile.Request{ClusterName: "arsys#account"}); err == nil {
		t.Fatal("incomplete inventory must retry")
	}
	if got := annotation(t, msp); got != "arsys,sap.eu" {
		t.Fatalf("startup wrote partial annotation: %q", got)
	}
	// Once all caches are synchronized, the complete inventory is safe.
	snapshot.err = nil
	snapshot.bindings = append(snapshot.bindings, binding("account", "sap", "sap.eu"))
	if err := c.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := annotation(t, msp); got != "arsys,sap.eu" {
		t.Fatalf("complete snapshot changed bindings: %q", got)
	}
	// A later authoritative unbind must still remove only the departed export.
	snapshot.bindings = snapshot.bindings[1:]
	if err := c.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := annotation(t, msp); got != "sap.eu" {
		t.Fatalf("valid unbind failed: %q", got)
	}
	// No remaining binding events are needed to clear the final annotation.
	snapshot.bindings = nil
	if err := c.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := annotation(t, msp); got != "" {
		t.Fatalf("last unbind failed: %q", got)
	}
}

func TestDisengagePreservesAnnotationOnTransportFailure(t *testing.T) {
	root := context.Background()
	msp := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(existingNamespace()).Build()
	c := New(root, nil, msp, index.New(), testConfig())
	snapshot := &snapshotInventory{err: errors.New("provider reconnecting"), called: make(chan struct{})}
	c.Inventory = snapshot
	ctx, cancel := context.WithCancel(root)
	if err := c.Engage(ctx, "sap#account", nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-snapshot.called:
	case <-time.After(time.Second):
		t.Fatal("disengage did not request an authoritative snapshot")
	}
	if got := annotation(t, msp); got != "arsys,sap.eu" {
		t.Fatalf("transport cancellation deleted exports: %q", got)
	}
}

func TestInvalidOrConflictingSnapshotPreservesAnnotation(t *testing.T) {
	for _, bindings := range [][]apisv1alpha1.APIBinding{
		{binding("", "bad", "sap.eu")},
		{binding("account", "same", "arsys"), binding("account", "same", "sap.eu")},
	} {
		msp := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(existingNamespace()).Build()
		c := New(context.Background(), nil, msp, index.New(), testConfig())
		c.Inventory = &snapshotInventory{bindings: bindings}
		if err := c.SyncAll(context.Background()); err == nil {
			t.Fatal("invalid inventory should fail closed")
		}
		if got := annotation(t, msp); got != "arsys,sap.eu" {
			t.Fatalf("invalid inventory changed annotation: %q", got)
		}
	}
}

func TestConcurrentEventsPublishCompleteSnapshots(t *testing.T) {
	msp := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(existingNamespace()).Build()
	c := New(context.Background(), nil, msp, index.New(), testConfig())
	c.Inventory = &snapshotInventory{bindings: []apisv1alpha1.APIBinding{binding("account", "arsys", "arsys"), binding("account", "sap", "sap.eu")}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.SyncAll(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := annotation(t, msp); got != "arsys,sap.eu" {
		t.Fatalf("concurrent events lost bindings: %q", got)
	}
}
