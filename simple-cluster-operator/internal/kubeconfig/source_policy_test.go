package kubeconfig

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/target"
)

func sourcePolicyFixture() (*showroomv1alpha1.SimpleCluster, *showroomv1alpha1.SimpleClusterProviderConfig, *showroomv1alpha1.ResourceSpec, *unstructured.Unstructured) {
	sc := &showroomv1alpha1.SimpleCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "account", Name: "demo"}}
	scpc := &showroomv1alpha1.SimpleClusterProviderConfig{Spec: showroomv1alpha1.SimpleClusterProviderConfigSpec{
		Target: showroomv1alpha1.TargetSpec{Namespace: "{{ .SC.namespace }}"},
		Inputs: []showroomv1alpha1.InputDeclaration{{Name: "secretNamespace", Default: "account"}, {Name: "secretName", Default: "key"}},
		KubeconfigSource: showroomv1alpha1.KubeconfigSource{ResourceName: "seed", SecretRef: &showroomv1alpha1.KubeconfigSecretSource{
			Name: "{{ .Inputs.secretName }}", Namespace: "{{ .Inputs.secretNamespace }}", DataKey: "password",
		}},
	}}
	resource := &showroomv1alpha1.ResourceSpec{Name: "seed", APIVersion: "v1", Kind: "ConfigMap"}
	live := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "demo-seed", "namespace": "account"}}}
	return sc, scpc, resource, live
}

func TestRenderedSecretSourceChecksBeforeRead(t *testing.T) {
	for _, tc := range []struct {
		label, namespace, name string
		entries                []secretpolicy.Entry
		allowed                bool
	}{
		{label: "own", namespace: "account", name: "key", allowed: true},
		{label: "foreign", namespace: "foreign", name: "key"},
		{label: "exact copy", namespace: "shared", name: "key", allowed: true, entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Copy}}},
		{label: "use cannot copy", namespace: "shared", name: "key", entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Use}}},
		{label: "wrong name", namespace: "shared", name: "other", entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Copy}}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			sc, scpc, resource, live := sourcePolicyFixture()
			sc.Spec.Inputs = map[string]string{"secretNamespace": tc.namespace, "secretName": tc.name}
			secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": tc.name, "namespace": tc.namespace}, "data": map[string]any{"password": base64.StdEncoding.EncodeToString([]byte("opaque-value"))}}}
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), secret)
			client := &target.Client{Dynamic: dynamic, SecretPolicy: secretpolicy.Policy{Allowlist: tc.entries}}
			data, _, err := Fetch(context.Background(), client, scpc, sc, resource, live)
			if tc.allowed {
				if err != nil || string(data) != "opaque-value" || len(dynamic.Actions()) != 1 {
					t.Fatalf("allowed source: data=%q, error=%v, actions=%v", data, err, dynamic.Actions())
				}
			} else if !errors.Is(err, secretpolicy.ErrDenied) || len(data) != 0 || len(dynamic.Actions()) != 0 {
				t.Fatalf("denied source reached API or returned bytes: error=%v, actions=%v", err, dynamic.Actions())
			}
		})
	}
}

func TestSecretSourceDefaultsToRequestNamespace(t *testing.T) {
	sc, scpc, resource, live := sourcePolicyFixture()
	scpc.Spec.KubeconfigSource.SecretRef.Namespace = ""
	live.SetNamespace("foreign")
	secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "key", "namespace": "account"}, "data": map[string]any{"password": base64.StdEncoding.EncodeToString([]byte("opaque-value"))}}}
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), secret)
	data, _, err := Fetch(context.Background(), &target.Client{Dynamic: dynamic}, scpc, sc, resource, live)
	if err != nil || string(data) != "opaque-value" {
		t.Fatalf("default namespace: data=%q, error=%v", data, err)
	}
	if len(dynamic.Actions()) != 1 || dynamic.Actions()[0].GetNamespace() != "account" {
		t.Fatalf("wrong namespace: %v", dynamic.Actions())
	}
}

func TestInlineSecretSourceCannotBypassCopyPolicy(t *testing.T) {
	sc, scpc, resource, live := sourcePolicyFixture()
	scpc.Spec.KubeconfigSource.SecretRef = nil
	scpc.Spec.KubeconfigSource.Inline = &showroomv1alpha1.KubeconfigInlineSource{KubeconfigPath: "data.password"}
	live.SetKind("Secret")
	live.Object["data"] = map[string]any{"password": "apiVersion: v1\nkind: Config\n"}
	for _, namespace := range []string{"foreign", "account"} {
		t.Run(namespace, func(t *testing.T) {
			live.SetNamespace(namespace)
			data, _, err := Fetch(context.Background(), &target.Client{}, scpc, sc, resource, live)
			if namespace == "foreign" {
				if !errors.Is(err, secretpolicy.ErrDenied) || len(data) != 0 {
					t.Fatalf("foreign inline Secret returned bytes: error=%v", err)
				}
			} else if err != nil || string(data) != "apiVersion: v1\nkind: Config\n" {
				t.Fatalf("own inline Secret failed: error=%v", err)
			}
		})
	}
}
