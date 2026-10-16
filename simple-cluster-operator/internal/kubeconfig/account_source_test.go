package kubeconfig

import (
	"context"
	"errors"
	"testing"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestAccountSecretSourceUsesAccountNamespacePolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	dyn := dynamicfake.NewSimpleDynamicClient(scheme, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shoot-key", Namespace: "default"}, Data: map[string][]byte{"kubeconfig": []byte("synthetic-access-fixture")}})
	tc := &target.Client{Dynamic: dyn, OwnerNamespace: "default", AccountParent: &showroomv1alpha1.AccountTargetStatus{Cluster: "account-a", Namespace: "default", UID: "parent"}}
	sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "account-a"}}
	rs := showroomv1alpha1.ResourceSpec{Name: "shoot", APIVersion: "sap.eu/v1alpha1", Kind: "IronCoreShoot", NameTemplate: "sc-child"}
	pc := &showroomv1alpha1.SimpleClusterProviderConfig{Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "shoot", SecretRef: &showroomv1alpha1.KubeconfigSecretSource{Name: "shoot-key", Namespace: "default"}}}}
	child := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "sap.eu/v1alpha1", "kind": "IronCoreShoot", "metadata": map[string]any{"name": "sc-child"}}}
	data, _, err := Fetch(context.Background(), tc, pc, sc, &rs, child)
	if err != nil || string(data) != "synthetic-access-fixture" {
		t.Fatal("account source read failed")
	}
	count := len(dyn.Actions())
	pc.Spec.KubeconfigSource.SecretRef.Namespace = "foreign"
	if _, _, err := Fetch(context.Background(), tc, pc, sc, &rs, child); !errors.Is(err, secretpolicy.ErrDenied) {
		t.Fatal("foreign account Secret namespace was accepted")
	}
	if len(dyn.Actions()) != count {
		t.Fatal("read Secret before account namespace authorization")
	}
}
