package target

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/internal/secretpolicy"
)

type secretReadSpy struct {
	client.Client
	reads int
}

func (s *secretReadSpy) Get(ctx context.Context, key types.NamespacedName, obj client.Object, options ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		s.reads++
	}
	return s.Client.Get(ctx, key, obj, options...)
}

func TestTargetReferenceRejectsBeforeReadAndNetwork(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, purpose := range []string{"", secretpolicy.Copy, secretpolicy.Write} {
		t.Run("exception_"+purpose, func(t *testing.T) {
			spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
			policy := secretpolicy.Policy{}
			if purpose != "" {
				policy.Allowlist = []secretpolicy.Entry{{Namespace: "foreign", Name: "key", Purpose: purpose}}
			}
			_, err := New(context.Background(), spy, &rest.Config{Host: server.URL}, nil,
				&corev1.SecretReference{Namespace: "foreign", Name: "key"}, "account", policy)
			if !errors.Is(err, secretpolicy.ErrDenied) {
				t.Fatalf("expected policy denial, got %v", err)
			}
			if spy.reads != 0 || calls.Load() != 0 {
				t.Fatalf("Secret reads=%d, network calls=%d", spy.reads, calls.Load())
			}
		})
	}
}

func TestTargetReferenceNormalizesAndAcceptsExactUse(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	for _, namespace := range []string{"", "account", "shared"} {
		t.Run("namespace_"+namespace, func(t *testing.T) {
			resolved := namespace
			if resolved == "" {
				resolved = "account"
			}
			// Invalid kubeconfig stops discovery after the allowed Secret read.
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: resolved, Name: "key"},
				Data: map[string][]byte{"kubeconfig": []byte("invalid")}}
			spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()}
			policy := secretpolicy.Policy{Allowlist: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Use}}}
			_, err := New(context.Background(), spy, nil, nil,
				&corev1.SecretReference{Namespace: namespace, Name: "key"}, "account", policy)
			if errors.Is(err, secretpolicy.ErrDenied) || spy.reads != 1 {
				t.Fatalf("allowed reference did not read its Secret: reads=%d, error=%v", spy.reads, err)
			}
		})
	}
}

func TestDynamicSecretReferenceChecksCopyAndWrite(t *testing.T) {
	mapping := &apimeta.RESTMapping{
		Resource: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, Scope: apimeta.RESTScopeNamespace,
	}
	for _, tc := range []struct {
		label, namespace, name, purpose string
		entries                         []secretpolicy.Entry
		allowed                         bool
	}{
		{label: "own copy", namespace: "account", name: "key", purpose: secretpolicy.Copy, allowed: true},
		{label: "default copy", name: "key", purpose: secretpolicy.Copy, allowed: true},
		{label: "foreign copy", namespace: "foreign", name: "key", purpose: secretpolicy.Copy},
		{label: "use does not copy", namespace: "shared", name: "key", purpose: secretpolicy.Copy,
			entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Use}}},
		{label: "exact copy", namespace: "shared", name: "key", purpose: secretpolicy.Copy, allowed: true,
			entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Copy}}},
		{label: "foreign write", namespace: "foreign", name: "key", purpose: secretpolicy.Write},
		{label: "copy does not write", namespace: "shared", name: "key", purpose: secretpolicy.Write,
			entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Copy}}},
		{label: "exact write", namespace: "shared", name: "key", purpose: secretpolicy.Write, allowed: true,
			entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Write}}},
		{label: "other name", namespace: "shared", name: "other", purpose: secretpolicy.Copy,
			entries: []secretpolicy.Entry{{Namespace: "shared", Name: "key", Purpose: secretpolicy.Copy}}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			target := &Client{Dynamic: dynamic, OwnerNamespace: "account", SecretPolicy: secretpolicy.Policy{Allowlist: tc.entries}}
			resource, err := target.ResourceFor(mapping, tc.namespace, tc.name, tc.purpose)
			if tc.allowed {
				if err != nil {
					t.Fatal(err)
				}
				_, _ = resource.Get(context.Background(), tc.name, metav1.GetOptions{})
				if len(dynamic.Actions()) != 1 {
					t.Fatal("allowed resource did not reach the client")
				}
			} else if !errors.Is(err, secretpolicy.ErrDenied) || resource != nil || len(dynamic.Actions()) != 0 {
				t.Fatalf("denial did not prevent client access: error=%v, actions=%v", err, dynamic.Actions())
			}
		})
	}
}
