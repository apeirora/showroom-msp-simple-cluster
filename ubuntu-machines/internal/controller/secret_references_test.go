package controller

import (
	"context"
	"errors"
	api "github.com/apeirora/showroom-msp-simple-cluster/ubuntu-machines/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/ubuntu-machines/internal/secretpolicy"
	gossh "golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func TestSecretAdminReference(t *testing.T) {
	for _, privateKey := range []bool{false, true} {
		for _, tc := range []struct {
			name, namespace, refName string
			policy                   secretpolicy.Policy
			allowed                  bool
		}{
			{"empty namespace", "", "admin", policyEntry("shared", "admin", secretpolicy.Use), false},
			{"no entry", "shared", "admin", secretpolicy.Policy{}, false},
			{"exact", "shared", "admin", policyEntry("shared", "admin", secretpolicy.Use), true},
			{"wrong name", "shared", "admin", policyEntry("shared", "other", secretpolicy.Use), false},
			{"wrong namespace", "foreign", "admin", policyEntry("shared", "admin", secretpolicy.Use), false},
			{"wrong purpose", "shared", "admin", policyEntry("shared", "admin", secretpolicy.Copy), false},
			{"empty name", "shared", "", policyEntry("shared", "admin", secretpolicy.Use), false},
		} {
			t.Run(tc.name+map[bool]string{false: " password", true: " private key"}[privateKey], func(t *testing.T) {
				m := &api.UbuntuMachine{ObjectMeta: metav1.ObjectMeta{Name: "machine"}, Spec: api.UbuntuMachineSpec{AdminSSH: api.SSHAuth{User: "root"}}}
				ref := &corev1.SecretReference{Namespace: tc.namespace, Name: tc.refName}
				if privateKey {
					m.Spec.AdminSSH.PrivateKeySecretRef = ref
				} else {
					m.Spec.AdminSSH.PasswordSecretRef = ref
				}
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: tc.namespace, Name: "admin"}}
				scheme := testScheme(t)
				spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(m).WithObjects(m, secret).Build()}
				calls := 0
				r := &UbuntuMachineClaimReconciler{Client: spy, Scheme: scheme, SecretPolicy: tc.policy, sshDial: func(*api.UbuntuMachine, *corev1.Secret) (*gossh.Client, error) {
					calls++
					return nil, errors.New("dial spy")
				}}
				_, err := r.getAdminSecret(context.Background(), m)
				if tc.allowed {
					if err != nil || len(spy.reads) != 1 {
						t.Fatalf("error=%v reads=%v", err, spy.reads)
					}
					return
				}
				if !errors.Is(err, secretpolicy.ErrDenied) || len(spy.reads) != 0 {
					t.Fatalf("error=%v reads=%v", err, spy.reads)
				}
				claim := &api.UbuntuMachineClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "account"}}
				if _, err = r.bind(context.Background(), claim, *m); !errors.Is(err, secretpolicy.ErrDenied) {
					t.Fatalf("bind error=%v", err)
				}
				var got api.UbuntuMachine
				if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(m), &got); err != nil {
					t.Fatal(err)
				}
				if got.Status.BoundClaimRef != nil {
					t.Fatal("machine was bound")
				}
				if _, err = r.markBound(context.Background(), claim, *m); !errors.Is(err, secretpolicy.ErrDenied) {
					t.Fatalf("markBound error=%v", err)
				}
				claim.Status.Machine = &api.BoundMachine{Name: m.Name}
				if err = r.removeClaimSSHKey(context.Background(), claim); !errors.Is(err, secretpolicy.ErrDenied) {
					t.Fatalf("remove error=%v", err)
				}
				if len(spy.reads) != 0 || calls != 0 {
					t.Fatalf("reads=%v dials=%d", spy.reads, calls)
				}
			})
		}
	}
}

func TestSecretAdminReconcileRejectsBeforeBind(t *testing.T) {
	scheme := testScheme(t)
	m := &api.UbuntuMachine{ObjectMeta: metav1.ObjectMeta{Name: "machine"}, Spec: api.UbuntuMachineSpec{AdminSSH: api.SSHAuth{PasswordSecretRef: &corev1.SecretReference{Namespace: "shared", Name: "admin"}}}}
	claim := &api.UbuntuMachineClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "account", Finalizers: []string{api.FinalizerName}}}
	spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(m, claim).WithObjects(m, claim).Build()}
	calls := 0
	r := &UbuntuMachineClaimReconciler{Client: spy, Scheme: scheme, sshDial: func(*api.UbuntuMachine, *corev1.Secret) (*gossh.Client, error) {
		calls++
		return nil, errors.New("dial spy")
	}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(claim)}); err != nil {
		t.Fatal(err)
	}
	var got api.UbuntuMachine
	if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(m), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.BoundClaimRef != nil || len(spy.reads) != 0 || calls != 0 {
		t.Fatalf("bound=%v reads=%v dials=%d", got.Status.BoundClaimRef, spy.reads, calls)
	}
}

func TestSecretAdminDeletionDenialKeepsOwnership(t *testing.T) {
	scheme := testScheme(t)
	now := metav1.Now()
	claim := &api.UbuntuMachineClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "account", Finalizers: []string{api.FinalizerName}, DeletionTimestamp: &now},
		Status:     api.UbuntuMachineClaimStatus{Machine: &api.BoundMachine{Name: "machine"}, SSHSecretRef: &corev1.LocalObjectReference{Name: "claim-ssh"}},
	}
	m := &api.UbuntuMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine"},
		Spec:       api.UbuntuMachineSpec{AdminSSH: api.SSHAuth{PasswordSecretRef: &corev1.SecretReference{Namespace: "shared", Name: "admin"}}},
		Status:     api.UbuntuMachineStatus{Phase: api.MachineBound, BoundClaimRef: &api.ClaimReference{Namespace: claim.Namespace, Name: claim.Name}},
	}
	key := &unstructured.Unstructured{}
	key.SetGroupVersionKind(sshKeyGVK)
	key.SetNamespace(claim.Namespace)
	key.SetName(claim.Name + "-sshkey")
	spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(m, claim).WithObjects(m, claim, key).Build()}
	calls := 0
	r := &UbuntuMachineClaimReconciler{Client: spy, Scheme: scheme, sshDial: func(*api.UbuntuMachine, *corev1.Secret) (*gossh.Client, error) {
		calls++
		return nil, errors.New("dial spy")
	}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(claim)}); !errors.Is(err, secretpolicy.ErrDenied) {
		t.Fatalf("error=%v", err)
	}
	var got api.UbuntuMachine
	if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(m), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != api.MachineBound || got.Status.BoundClaimRef == nil {
		t.Fatal("machine was released")
	}
	if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(key), key); err != nil {
		t.Fatalf("SSHKey not retained: %v", err)
	}
	var kept api.UbuntuMachineClaim
	if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(claim), &kept); err != nil {
		t.Fatal(err)
	}
	if len(kept.Finalizers) != 1 || kept.Finalizers[0] != api.FinalizerName || len(spy.reads) != 0 || calls != 0 {
		t.Fatalf("finalizers=%v reads=%v dials=%d", kept.Finalizers, spy.reads, calls)
	}
}

func TestSecretAdminSelectionUsesAuthorizedAlternative(t *testing.T) {
	scheme := testScheme(t)
	claim := &api.UbuntuMachineClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "account", Finalizers: []string{api.FinalizerName}}}
	denied := &api.UbuntuMachine{ObjectMeta: metav1.ObjectMeta{Name: "a-denied"}, Spec: api.UbuntuMachineSpec{AdminSSH: api.SSHAuth{PasswordSecretRef: &corev1.SecretReference{Namespace: "shared", Name: "denied"}}}}
	allowed := denied.DeepCopy()
	allowed.Name = "b-allowed"
	allowed.Spec.AdminSSH.PasswordSecretRef.Name = "allowed"
	spy := &secretReadSpy{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim, denied, allowed).WithObjects(claim, denied, allowed).Build()}
	r := &UbuntuMachineClaimReconciler{Client: spy, Scheme: scheme, SecretPolicy: policyEntry("shared", "allowed", secretpolicy.Use)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(claim)}); err != nil {
		t.Fatal(err)
	}
	if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(denied), denied); err != nil {
		t.Fatal(err)
	}
	if err := spy.Client.Get(context.Background(), client.ObjectKeyFromObject(allowed), allowed); err != nil {
		t.Fatal(err)
	}
	if denied.Status.BoundClaimRef != nil || allowed.Status.BoundClaimRef == nil || allowed.Status.BoundClaimRef.Name != claim.Name {
		t.Fatalf("denied=%v allowed=%v", denied.Status.BoundClaimRef, allowed.Status.BoundClaimRef)
	}
	if len(spy.reads) != 0 {
		t.Fatal(spy.reads)
	}
}
