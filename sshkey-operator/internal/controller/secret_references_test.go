package controller

import (
	"context"
	api "github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/internal/secretpolicy"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

type secretReadSpy struct {
	client.Client
	reads []types.NamespacedName
}

func (s *secretReadSpy) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		s.reads = append(s.reads, key)
	}
	return s.Client.Get(ctx, key, obj, opts...)
}
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}
func policyEntry(namespace, name, purpose string) secretpolicy.Policy {
	return secretpolicy.Policy{Allowlist: []secretpolicy.Entry{{Namespace: namespace, Name: name, Purpose: purpose}}}
}

func TestSecretPassphraseReference(t *testing.T) {
	for _, tc := range []struct {
		name, namespace, refName string
		policy                   secretpolicy.Policy
		allowed                  bool
	}{
		{"default", "", "pass", secretpolicy.Policy{}, true}, {"own", "account", "pass", secretpolicy.Policy{}, true},
		{"foreign", "shared", "pass", secretpolicy.Policy{}, false},
		{"exact", "shared", "pass", policyEntry("shared", "pass", secretpolicy.Use), true},
		{"wrong name", "shared", "pass", policyEntry("shared", "other", secretpolicy.Use), false},
		{"wrong purpose", "shared", "pass", policyEntry("shared", "pass", secretpolicy.Copy), false},
		{"namespace without name", "shared", "", secretpolicy.Policy{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &api.SSHKey{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "account"}, Spec: api.SSHKeySpec{PassPhaseSecret: corev1.SecretReference{Namespace: tc.namespace, Name: tc.refName}, ExportToSecret: corev1.LocalObjectReference{Name: "export"}}}
			ns := tc.namespace
			if ns == "" {
				ns = "account"
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "pass"}, Data: map[string][]byte{"password": []byte("value")}}
			scheme := testScheme(t)
			spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(key).WithObjects(key, secret).Build()}
			r := &SSHKeyReconciler{Client: spy, Scheme: scheme, SecretPolicy: tc.policy}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: key.Namespace, Name: key.Name}}); err != nil {
				t.Fatal(err)
			}
			var got api.SSHKey
			if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(key), &got); err != nil {
				t.Fatal(err)
			}
			var exported corev1.Secret
			exportErr := spy.Client.Get(context.Background(), types.NamespacedName{Namespace: "account", Name: "export"}, &exported)
			if tc.allowed {
				if !got.Status.Ready || len(spy.reads) != 2 || spy.reads[0].Namespace != ns || exportErr != nil {
					t.Fatalf("ready=%t reads=%v exportErr=%v", got.Status.Ready, spy.reads, exportErr)
				}
			} else if got.Status.Ready || got.PrivateKey != "" || len(spy.reads) != 0 || exportErr == nil {
				t.Fatalf("ready=%t key generated=%t reads=%v exportErr=%v", got.Status.Ready, got.PrivateKey != "", spy.reads, exportErr)
			}
		})
	}
}
