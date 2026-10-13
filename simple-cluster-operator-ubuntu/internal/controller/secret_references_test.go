package controller

import (
	"context"
	"errors"
	api "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/secretpolicy"
	sshpkg "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/ssh"
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

func clusterFixture() *api.UbuntuCluster {
	return &api.UbuntuCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "account", Finalizers: []string{api.FinalizerName}},
		Spec: api.UbuntuClusterSpec{
			SSH:          api.SSHAuth{User: "root", PasswordSecretRef: &corev1.SecretReference{Name: "login"}},
			ControlPlane: api.HostPool{Hosts: []api.Host{{Name: "cp", Address: "127.0.0.1"}}},
			Workers:      api.HostPool{Hosts: []api.Host{{Name: "worker", Address: "127.0.0.2"}}},
		},
	}
}
func TestSecretReferencesRejectBeforeReadOrDial(t *testing.T) {
	cases := []struct {
		name   string
		change func(*api.UbuntuCluster)
	}{
		{"private key", func(c *api.UbuntuCluster) {
			c.Spec.SSH.PasswordSecretRef = nil
			c.Spec.SSH.PrivateKeySecretRef = &corev1.SecretReference{Namespace: "foreign", Name: "login"}
		}},
		{"password", func(c *api.UbuntuCluster) { c.Spec.SSH.PasswordSecretRef.Namespace = "foreign" }},
		{"bootstrap", func(c *api.UbuntuCluster) {
			c.Spec.ControlPlane.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Namespace: "foreign", Name: "login"}
		}},
		{"secondary control plane", func(c *api.UbuntuCluster) {
			c.Spec.ControlPlane.Hosts = append(c.Spec.ControlPlane.Hosts, api.Host{Name: "cp2", PasswordSecretRef: &corev1.SecretReference{Namespace: "foreign", Name: "login"}})
		}},
		{"worker", func(c *api.UbuntuCluster) {
			c.Spec.Workers.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Namespace: "foreign", Name: "login"}
		}},
		{"empty name", func(c *api.UbuntuCluster) { c.Spec.SSH.PasswordSecretRef.Name = "" }},
		{"both refs", func(c *api.UbuntuCluster) { c.Spec.SSH.PrivateKeySecretRef = &corev1.SecretReference{Name: "login"} }},
	}
	for _, tc := range cases {
		for _, deleting := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: " create", true: " delete"}[deleting], func(t *testing.T) {
				c := clusterFixture()
				tc.change(c)
				if deleting {
					now := metav1.Now()
					c.DeletionTimestamp = &now
				}
				scheme := testScheme(t)
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "account", Name: "login"}, Data: map[string][]byte{"password": []byte("value")}}
				spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(c).WithObjects(c, secret).Build()}
				calls := 0
				r := &UbuntuClusterReconciler{Client: spy, Scheme: scheme,
					sshDial: func(context.Context, string, int, string, sshpkg.Credential) (*sshpkg.Client, error) {
						calls++
						return nil, errors.New("dial spy")
					},
					sshDialVia: func(context.Context, *sshpkg.Client, string, int, string, sshpkg.Credential) (*sshpkg.Client, error) {
						calls++
						return nil, errors.New("dial spy")
					},
				}
				if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: c.Namespace, Name: c.Name}}); err != nil {
					t.Fatal(err)
				}
				if len(spy.reads) != 0 || calls != 0 {
					t.Fatalf("reads=%v dials=%d", spy.reads, calls)
				}
			})
		}
	}
}
func TestSecretCredentialResolution(t *testing.T) {
	for _, tc := range []struct {
		name, namespace string
		policy          secretpolicy.Policy
		allowed         bool
	}{
		{"default", "", secretpolicy.Policy{}, true}, {"own", "account", secretpolicy.Policy{}, true},
		{"foreign", "shared", secretpolicy.Policy{}, false},
		{"exact", "shared", policyEntry("shared", "login", secretpolicy.Use), true},
		{"wrong name", "shared", policyEntry("shared", "other", secretpolicy.Use), false},
		{"wrong purpose", "shared", policyEntry("shared", "login", secretpolicy.Copy), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clusterFixture()
			c.Spec.SSH.PasswordSecretRef.Namespace = tc.namespace
			c.Spec.Workers.Hosts[0].PasswordSecretRef = &corev1.SecretReference{Namespace: tc.namespace, Name: "login"}
			ns := tc.namespace
			if ns == "" {
				ns = "account"
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "login"}, Data: map[string][]byte{"password": []byte("value"), "username": []byte("operator")}}
			spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(secret).Build()}
			r := &UbuntuClusterReconciler{Client: spy, SecretPolicy: tc.policy}
			cred, err := r.readSSHCredential(context.Background(), c)
			if tc.allowed {
				if err != nil || cred.Password != "value" {
					t.Fatalf("credential=%v error=%v", cred, err)
				}
			} else if !errors.Is(err, secretpolicy.ErrDenied) {
				t.Fatalf("error=%v", err)
			}
			user, hostCred, err := r.hostCredential(context.Background(), c, c.Spec.Workers.Hosts[0], cred)
			if tc.allowed {
				if err != nil || hostCred.Password != "value" || user != "operator" {
					t.Fatalf("user=%s credential=%v error=%v", user, hostCred, err)
				}
				if len(spy.reads) != 2 || spy.reads[0].Namespace != ns || spy.reads[1].Namespace != ns {
					t.Fatal(spy.reads)
				}
			} else if !errors.Is(err, secretpolicy.ErrDenied) || len(spy.reads) != 0 {
				t.Fatalf("error=%v reads=%v", err, spy.reads)
			}
		})
	}
}
