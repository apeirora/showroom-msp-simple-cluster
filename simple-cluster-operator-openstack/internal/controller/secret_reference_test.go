package controller

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	openstackv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-openstack/internal/secretpolicy"
)

type referenceSpy struct {
	client.Client
	reads            []types.NamespacedName
	creates, patches int
}

func (s *referenceSpy) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	s.reads = append(s.reads, key)
	return s.Client.Get(ctx, key, obj, opts...)
}
func (s *referenceSpy) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	s.creates++
	return s.Client.Create(ctx, obj, opts...)
}
func (s *referenceSpy) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	s.patches++
	return s.Client.Patch(ctx, obj, patch, opts...)
}
func referenceClient(t *testing.T, objs ...client.Object) (*referenceSpy, *runtime.Scheme) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := openstackv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&openstackv1alpha1.OpenStackVM{}).WithObjects(objs...).Build()
	return &referenceSpy{Client: c}, scheme
}
func referenceVM() *openstackv1alpha1.OpenStackVM {
	return &openstackv1alpha1.OpenStackVM{ObjectMeta: metav1.ObjectMeta{Name: "vm", Namespace: "tenant"}, Spec: openstackv1alpha1.OpenStackVMSpec{CredentialsRef: corev1.SecretReference{Name: "creds"}}}
}
func assertDenied(t *testing.T, err error, spy *referenceSpy, reads int) {
	t.Helper()
	if !errors.Is(err, secretpolicy.ErrDenied) {
		t.Fatalf("error=%v, want policy denial", err)
	}
	if len(spy.reads) != reads || spy.creates != 0 || spy.patches != 0 {
		t.Fatalf("reads=%v creates=%d patches=%d", spy.reads, spy.creates, spy.patches)
	}
}
func TestVMReferencePreflight(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		for _, kind := range []string{"credentials", "Secret", "SSHKey"} {
			t.Run(kind+map[bool]string{true: "/delete", false: "/reconcile"}[deleting], func(t *testing.T) {
				vm := referenceVM()
				switch kind {
				case "credentials":
					vm.Spec.CredentialsRef.Namespace = "other"
				case "Secret":
					vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{Kind: "Secret", Name: "shared", Namespace: ptr("other")}
				case "SSHKey":
					vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{APIGroup: ptr(openstackv1alpha1.SSHKeyAPIGroup), Kind: "SSHKey", Name: "shared", Namespace: ptr("other")}
				}
				if deleting {
					now := metav1.Now()
					vm.DeletionTimestamp = &now
					vm.Finalizers = []string{openstackv1alpha1.FinalizerName}
				}
				spy, scheme := referenceClient(t, vm)
				r := &OpenStackVMReconciler{Client: spy, Scheme: scheme}
				_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(vm)})
				assertDenied(t, err, spy, 1)
			})
		}
	}
}
func TestDirectReferenceGuards(t *testing.T) {
	t.Run("credentials", func(t *testing.T) {
		vm := referenceVM()
		vm.Spec.CredentialsRef.Namespace = "other"
		spy, scheme := referenceClient(t)
		r := &OpenStackVMReconciler{Client: spy, Scheme: scheme}
		_, err := r.openstackClients(context.Background(), vm)
		assertDenied(t, err, spy, 0)
	})
	for _, kind := range []string{"Secret", "SSHKey"} {
		t.Run(kind, func(t *testing.T) {
			vm := referenceVM()
			vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{Kind: kind, Name: "shared", Namespace: ptr("other")}
			if kind == "SSHKey" {
				vm.Spec.SSHKeyRef.APIGroup = ptr(openstackv1alpha1.SSHKeyAPIGroup)
			}
			spy, scheme := referenceClient(t)
			r := &OpenStackVMReconciler{Client: spy, Scheme: scheme}
			_, _, err := r.ensureSSHKeypair(context.Background(), vm)
			assertDenied(t, err, spy, 0)
			if kind == "SSHKey" {
				_, _, err = r.resolveSSHKeyResource(context.Background(), vm)
				assertDenied(t, err, spy, 0)
			}
		})
	}
}
func TestSSHSecretReferencePolicy(t *testing.T) {
	for _, ns := range []string{"", "tenant", "other"} {
		for _, existing := range []bool{false, true} {
			t.Run(ns+map[bool]string{true: "/existing", false: "/create"}[existing], func(t *testing.T) {
				vm := referenceVM()
				vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{Kind: "Secret", Name: "shared", Namespace: ptr(ns)}
				target := ns
				if target == "" {
					target = vm.Namespace
				}
				var objs []client.Object
				if existing {
					objs = append(objs, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: target}, Data: map[string][]byte{openstackv1alpha1.SSHSecretPublicKeyKey: []byte("ssh-ed25519 test")}})
				}
				spy, scheme := referenceClient(t, objs...)
				r := &OpenStackVMReconciler{Client: spy, Scheme: scheme}
				if target == "other" {
					r.SecretPolicy.Allowlist = []secretpolicy.Entry{{Namespace: target, Name: "shared", Purpose: secretpolicy.Use}, {Namespace: target, Name: "shared", Purpose: secretpolicy.Write}, {Namespace: "tenant", Name: "creds", Purpose: secretpolicy.Use}}
				}
				ref, _, err := r.ensureSSHKeypair(context.Background(), vm)
				if err != nil {
					t.Fatal(err)
				}
				if ref.Namespace != target || len(spy.reads) != 1 {
					t.Fatalf("ref=%v reads=%v", ref, spy.reads)
				}
				if existing && spy.patches != 1 || !existing && spy.creates != 1 {
					t.Fatalf("creates=%d patches=%d", spy.creates, spy.patches)
				}
			})
		}
	}
}
func TestReadExceptionDoesNotPermitWrite(t *testing.T) {
	vm := referenceVM()
	vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{Kind: "Secret", Name: "shared", Namespace: ptr("other")}
	spy, scheme := referenceClient(t)
	r := &OpenStackVMReconciler{Client: spy, Scheme: scheme, SecretPolicy: secretpolicy.Policy{Allowlist: []secretpolicy.Entry{{Namespace: "other", Name: "shared", Purpose: secretpolicy.Use}}}}
	_, _, err := r.ensureSSHKeypair(context.Background(), vm)
	assertDenied(t, err, spy, 0)
}
func TestSSHSecretCleanupReferences(t *testing.T) {
	for _, ns := range []string{"", "tenant", "other"} {
		for _, allowed := range []bool{false, true} {
			t.Run(ns+map[bool]string{true: "/exception", false: "/default"}[allowed], func(t *testing.T) {
				now := metav1.Now()
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ssh", Namespace: "tenant", DeletionTimestamp: &now, Finalizers: []string{openstackv1alpha1.KeypairFinalizerName}, Annotations: map[string]string{openstackv1alpha1.SSHSecretCredentialsNameAnno: "creds", openstackv1alpha1.SSHSecretCredentialsNamespaceAnno: ns, openstackv1alpha1.SSHSecretKeypairNameAnno: "keypair"}}}
				spy, scheme := referenceClient(t, secret)
				r := &SSHSecretReconciler{Client: spy, Scheme: scheme}
				if allowed {
					r.SecretPolicy.Allowlist = []secretpolicy.Entry{{Namespace: "other", Name: "creds", Purpose: secretpolicy.Use}}
				}
				_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(secret)})
				if ns == "other" && !allowed {
					assertDenied(t, err, spy, 1)
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				target := ns
				if target == "" {
					target = "tenant"
				}
				if len(spy.reads) != 2 || spy.reads[1] != (types.NamespacedName{Namespace: target, Name: "creds"}) || spy.patches != 1 {
					t.Fatalf("reads=%v patches=%d", spy.reads, spy.patches)
				}
			})
		}
	}
}

func TestSSHKeyExportReferencePolicy(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{true: "exact-export", false: "object-only"}[allowed], func(t *testing.T) {
			vm := referenceVM()
			vm.Spec.SSHKeyRef = &corev1.TypedObjectReference{APIGroup: ptr(openstackv1alpha1.SSHKeyAPIGroup), Kind: "SSHKey", Name: "key", Namespace: ptr("other")}
			key := newSSHKey("other", "key", "ssh-ed25519 test", "export")
			sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "export", Namespace: "other"}}
			spy, scheme := referenceClient(t, key, sec)
			r := &OpenStackVMReconciler{Client: spy, Scheme: scheme, SecretPolicy: secretpolicy.Policy{Allowlist: []secretpolicy.Entry{{Namespace: "other", Name: "key", Purpose: secretpolicy.Use}}}}
			if allowed {
				r.SecretPolicy.Allowlist = append(r.SecretPolicy.Allowlist, secretpolicy.Entry{Namespace: "other", Name: "export", Purpose: secretpolicy.Use}, secretpolicy.Entry{Namespace: "other", Name: "export", Purpose: secretpolicy.Write}, secretpolicy.Entry{Namespace: "tenant", Name: "creds", Purpose: secretpolicy.Use})
			}
			ref, _, err := r.ensureSSHKeypair(context.Background(), vm)
			if !allowed {
				assertDenied(t, err, spy, 1)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ref.Name != "export" || ref.Namespace != "other" || len(spy.reads) != 2 || spy.patches != 1 {
				t.Fatalf("ref=%v reads=%v patches=%d", ref, spy.reads, spy.patches)
			}
		})
	}
}
func TestBookkeepingDirectGuard(t *testing.T) {
	vm := referenceVM()
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "other"}}
	spy, scheme := referenceClient(t)
	r := &OpenStackVMReconciler{Client: spy, Scheme: scheme}
	err := r.ensureSecretBookkeeping(context.Background(), sec, vm, client.ObjectKeyFromObject(sec), false)
	assertDenied(t, err, spy, 0)
	if len(sec.Finalizers) != 0 || len(sec.Annotations) != 0 {
		t.Fatal("denied bookkeeping changed the Secret")
	}
}
func TestCredentialExceptionIsExact(t *testing.T) {
	for _, entry := range []secretpolicy.Entry{
		{Namespace: "other", Name: "different", Purpose: secretpolicy.Use},
		{Namespace: "different", Name: "creds", Purpose: secretpolicy.Use},
		{Namespace: "other", Name: "creds", Purpose: secretpolicy.Write},
		{Namespace: "other", Name: "creds", Purpose: secretpolicy.Use},
	} {
		t.Run(entry.Namespace+"/"+entry.Name+"/"+entry.Purpose, func(t *testing.T) {
			vm := referenceVM()
			vm.Spec.CredentialsRef.Namespace = "other"
			spy, scheme := referenceClient(t)
			r := &OpenStackVMReconciler{Client: spy, Scheme: scheme, SecretPolicy: secretpolicy.Policy{Allowlist: []secretpolicy.Entry{entry}}}
			_, err := r.openstackClients(context.Background(), vm)
			if entry.Namespace != "other" || entry.Name != "creds" || entry.Purpose != secretpolicy.Use {
				assertDenied(t, err, spy, 0)
				return
			}
			if errors.Is(err, secretpolicy.ErrDenied) || len(spy.reads) != 1 || spy.reads[0] != (types.NamespacedName{Namespace: "other", Name: "creds"}) {
				t.Fatalf("error=%v reads=%v", err, spy.reads)
			}
		})
	}
}
