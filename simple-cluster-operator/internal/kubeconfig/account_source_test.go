package kubeconfig

import (
	"context"
	"encoding/base64"
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
	for _, group := range []string{"sap.eu", "apeiro.eu"} {
		t.Run(group, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			dyn := dynamicfake.NewSimpleDynamicClient(scheme, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shoot-key", Namespace: "default"}, Data: map[string][]byte{"kubeconfig": []byte("synthetic-access-fixture")}})
			tc := &target.Client{Dynamic: dyn, OwnerNamespace: "default", AccountParent: &showroomv1alpha1.AccountTargetStatus{Cluster: "account-a", Namespace: "default", UID: "parent"}}
			sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "account-a"}}
			rs := showroomv1alpha1.ResourceSpec{Name: "shoot", APIVersion: group + "/v1alpha1", Kind: "IronCoreShoot", NameTemplate: "sc-child"}
			pc := &showroomv1alpha1.SimpleClusterProviderConfig{Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "shoot", SecretRef: &showroomv1alpha1.KubeconfigSecretSource{Name: "shoot-key", Namespace: "default"}}}}
			child := &unstructured.Unstructured{Object: map[string]any{"apiVersion": group + "/v1alpha1", "kind": "IronCoreShoot", "metadata": map[string]any{"name": "sc-child"}}}
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
		})
	}

}

func TestAccountInlineSecretUsesAccountNamespace(t *testing.T) {
	const fixture = "apiVersion: v1\nkind: Config\nusers: []\n"
	for _, account := range []bool{true, false} {
		tc := &target.Client{OwnerNamespace: "default"}
		namespace := "account-a"
		if account {
			tc.AccountParent = &showroomv1alpha1.AccountTargetStatus{Cluster: "account-a", Namespace: "default", UID: "parent"}
			namespace = "default"
		}
		sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "account-a"}}
		pc := &showroomv1alpha1.SimpleClusterProviderConfig{Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{KubeconfigSource: showroomv1alpha1.KubeconfigSource{Inline: &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.kubeconfig"}}}}
		child := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "child-key", "namespace": namespace}, "data": map[string]any{"kubeconfig": base64.StdEncoding.EncodeToString([]byte(fixture))}}}
		data, _, err := Fetch(context.Background(), tc, pc, sc, &showroomv1alpha1.ResourceSpec{}, child)
		if err != nil || string(data) != fixture {
			t.Fatalf("account=%v inline Secret copy failed: %v", account, err)
		}
		child.SetNamespace("foreign")
		if _, _, err := Fetch(context.Background(), tc, pc, sc, &showroomv1alpha1.ResourceSpec{}, child); !errors.Is(err, secretpolicy.ErrDenied) {
			t.Fatalf("account=%v foreign inline Secret accepted", account)
		}
	}
}

func TestAccountSecretSourceDefaultsToAccountNamespace(t *testing.T) {
	for _, account := range []bool{true, false} {
		scheme := runtime.NewScheme()
		if err := corev1.AddToScheme(scheme); err != nil {
			t.Fatal(err)
		}
		namespace := "account-a"
		tc := &target.Client{OwnerNamespace: "default"}
		if account {
			tc.AccountParent = &showroomv1alpha1.AccountTargetStatus{Cluster: "account-a", Namespace: "default", UID: "parent"}
			namespace = "default"
		}
		tc.Dynamic = dynamicfake.NewSimpleDynamicClient(scheme, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "child-key", Namespace: namespace}, Data: map[string][]byte{"kubeconfig": []byte("synthetic-access-fixture")}})
		sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "account-a"}}
		rs := &showroomv1alpha1.ResourceSpec{Name: "child", APIVersion: "apeiro.eu/v1alpha1", Kind: "IronCoreShoot"}
		pc := &showroomv1alpha1.SimpleClusterProviderConfig{Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "child", SecretRef: &showroomv1alpha1.KubeconfigSecretSource{Name: "child-key"}}}}
		child := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apeiro.eu/v1alpha1", "kind": "IronCoreShoot", "metadata": map[string]any{"name": "child"}}}
		data, _, err := Fetch(context.Background(), tc, pc, sc, rs, child)
		if err != nil || string(data) != "synthetic-access-fixture" {
			t.Fatalf("account=%v omitted namespace read failed: %v", account, err)
		}
		actions := tc.Dynamic.(*dynamicfake.FakeDynamicClient).Actions()
		if len(actions) != 1 || actions[0].GetNamespace() != namespace {
			t.Fatalf("account=%v Secret read used the wrong namespace", account)
		}
	}
}
