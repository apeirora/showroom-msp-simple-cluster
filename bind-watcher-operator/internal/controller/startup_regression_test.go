package controller

import (
	"context"
	"testing"

	"github.com/apeirora/showroom-msp-simple-cluster/bind-watcher-operator/internal/index"
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type startupManager struct {
	mcmanager.Manager
	cl cluster.Cluster
}

func (m startupManager) GetCluster(context.Context, string) (cluster.Cluster, error) {
	return m.cl, nil
}

type startupCluster struct {
	cluster.Cluster
	cl client.Client
}

func (c startupCluster) GetClient() client.Client { return c.cl }

// This regression also compiles unchanged against v0.37.1. On that tag the
// first arsys event replaces the complete old annotation with just "arsys".
func TestStartupEventCannotRemoveExistingOtherExports(t *testing.T) {
	ctx := context.Background()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "account", Labels: map[string]string{testConfig().ClusterLabelKey: "account"},
		Annotations: map[string]string{testConfig().ExportsAnnotationKey: "arsys,sap.eu"}}}
	msp := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(ns).Build()
	b := &apisv1alpha1.APIBinding{ObjectMeta: metav1.ObjectMeta{Name: "arsys", Annotations: map[string]string{"kcp.io/cluster": "account"}},
		Spec: apisv1alpha1.APIBindingSpec{Reference: apisv1alpha1.BindingReference{Export: &apisv1alpha1.ExportBindingReference{Name: "arsys"}}}}
	kcp := fake.NewClientBuilder().WithScheme(Scheme).WithObjects(b).Build()
	mgr := startupManager{cl: startupCluster{cl: kcp}}
	c := New(ctx, mgr, msp, index.New(), testConfig())
	// Retrying until inventory readiness is valid; deleting another export is not.
	_, _ = c.Reconcile(ctx, mcreconcile.Request{ClusterName: "arsys#account", Request: ctrl.Request{NamespacedName: types.NamespacedName{Name: "arsys"}}})
	got := &corev1.Namespace{}
	if err := msp.Get(ctx, client.ObjectKey{Name: "account"}, got); err != nil {
		t.Fatal(err)
	}
	if actual := got.Annotations[testConfig().ExportsAnnotationKey]; actual != "arsys,sap.eu" {
		t.Fatalf("first startup event destroyed another provider's discovery: got %q, want arsys,sap.eu", actual)
	}
}
